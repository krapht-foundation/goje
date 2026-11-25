package skirmish

import (
	"bytes"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

func TestEventsReportEachKind(t *testing.T) {
	const alarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 32}`
	for _, radius := range []int{0, 7} {
		s := build(t, open(12, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 11, 0}}, alarm)
		if radius != 0 {
			withVision(t, s, radius)
		}
		s.Command(1, grid.Cell{X: 4, Y: 0})
		count := map[EventKind]int{}
		hp := map[uint64]int{1: scoutHP, 2: guardHP}
		for range 400 {
			step(t, s, 1)
			hits := map[uint64]int{}
			for _, e := range s.Events() {
				count[e.Kind]++
				u := unit(t, s, e.Unit)
				// Nothing moves after the events of a tick, so each names
				// where its unit stands now.
				if e.At != u.Center() {
					t.Fatalf("radius %d: event %d for unit %d at %v, unit at %v",
						radius, e.Kind, e.Unit, e.At, u.Center())
				}
				switch e.Kind {
				case EventAlarm:
					if e.Unit != 1 || !s.Alarm() {
						t.Fatalf("radius %d: the alarm names unit %d", radius, e.Unit)
					}
				case EventBoltHit:
					hits[e.Unit]++
				case EventUnitFell:
					if u.Alive || u.HP > 0 {
						t.Fatalf("radius %d: unit %d fell but lives", radius, e.Unit)
					}
				case EventBoltFired:
					if u.cooldown != weaponCooldown {
						t.Fatalf("radius %d: unit %d fired without spending its weapon", radius, e.Unit)
					}
				}
			}
			for id, before := range hp {
				if now := unit(t, s, id).HP; before-now != weaponDamage*hits[id] {
					t.Fatalf(
						"radius %d: unit %d lost %d health with %d hits",
						radius,
						id,
						before-now,
						hits[id],
					)
				}
				hp[id] = unit(t, s, id).HP
			}
		}
		if count[EventAlarm] != 1 || count[EventBoltFired] == 0 || count[EventBoltHit] == 0 ||
			count[EventUnitFell] != 1 {
			t.Fatalf("radius %d: events %v, want one alarm, one fall, and shots and hits", radius, count)
		}
		if len(s.Events()) != 0 {
			t.Fatalf("radius %d: a quiet tick kept %d events", radius, len(s.Events()))
		}
	}
}

// TestReadingEventsChangesNothing runs one history twice, reading the events
// after every tick in one run only, and compares the traces and snapshots.
func TestReadingEventsChangesNothing(t *testing.T) {
	for _, radius := range []int{0, 4} {
		h := newOrderHistory(5)
		h.radius = radius
		quiet, reader := h.build(t), h.build(t)
		h.run(t, quiet, 0, 900)
		seen := 0
		for tick := range 900 {
			h.run(t, reader, tick, tick+1)
			seen += len(reader.Events())
			for i := range reader.Events() {
				_ = reader.Events()[i].At
			}
		}
		a, _ := quiet.Snapshot()
		b, _ := reader.Snapshot()
		if seen == 0 || quiet.Trace() != reader.Trace() || !bytes.Equal(a, b) {
			t.Fatalf("radius %d: reading %d events changed the run", radius, seen)
		}
	}
}

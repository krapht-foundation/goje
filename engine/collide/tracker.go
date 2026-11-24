package collide

import (
	"cmp"
	"slices"
)

// Event reports that a body entered or left a trigger.
type Event struct {
	Trigger uint64
	Body    uint64
	// Enter is true when the body started overlapping the trigger and false
	// when it stopped.
	Enter bool
}

// pair is one body overlapping one trigger.
type pair struct{ trigger, body uint64 }

func comparePairs(a, b pair) int {
	if c := cmp.Compare(a.trigger, b.trigger); c != 0 {
		return c
	}
	return cmp.Compare(a.body, b.body)
}

// Tracker turns the triggers each body overlaps into enter and exit events. It
// remembers which pairs overlapped at the previous Update, so it belongs to one
// scene and must be replaced, or constructed again, when the scene restarts. A
// Tracker must not be copied or used concurrently.
type Tracker struct {
	world     *World
	prev, cur []pair
}

// NewTracker returns a tracker with no remembered overlaps for world.
func NewTracker(world *World) *Tracker {
	return &Tracker{world: world}
}

// Update compares the triggers the bodies overlap now with those from the
// previous Update and appends the differences to dst, ordered by trigger ID
// and then body ID. The caller owns dst and can reuse its capacity. The first
// Update reports only entries, including a body that spawns inside a trigger.
// A body missing from a later call exits everything it overlapped, so a game
// can despawn a body by leaving it out. Body IDs must be unique; a repeated ID
// is treated as one body that overlaps what any of its copies overlap.
//
// Overlap uses the same rules as World.Overlaps, so a body that only touches a
// trigger's edge, or passes under it, is not inside it. Update does not
// allocate once its internal buffers have grown to fit the busiest tick.
func (t *Tracker) Update(dst []Event, bodies []Shape) []Event {
	t.cur = t.cur[:0]
	for i := range bodies {
		body := &bodies[i]
		t.world.triggers.each(t.world, body.Footprint, body.Vertical, func(s *Shape) bool {
			t.cur = append(t.cur, pair{trigger: s.ID, body: body.ID})
			return true
		})
	}
	slices.SortFunc(t.cur, comparePairs)
	t.cur = slices.Compact(t.cur)

	i, j := 0, 0
	for i < len(t.prev) || j < len(t.cur) {
		switch {
		case j == len(t.cur) || (i < len(t.prev) && comparePairs(t.prev[i], t.cur[j]) < 0):
			dst = append(dst, Event{Trigger: t.prev[i].trigger, Body: t.prev[i].body})
			i++
		case i == len(t.prev) || comparePairs(t.cur[j], t.prev[i]) < 0:
			dst = append(dst, Event{Trigger: t.cur[j].trigger, Body: t.cur[j].body, Enter: true})
			j++
		default:
			i++
			j++
		}
	}
	t.prev, t.cur = t.cur, t.prev
	return dst
}

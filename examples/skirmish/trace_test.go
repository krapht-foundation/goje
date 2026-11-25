package skirmish

import (
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

// The traces below are the contract that one input history gives one result on
// every target. They change only when the rules do, and the Wasm test run must
// reproduce them. Update them together with the specification.
const (
	wantCrowdTrace  uint64 = 8444938424902188760
	wantSquadsTrace uint64 = 14811442038904706399
	wantOrdersTrace uint64 = 16928502377189695761
)

func TestCrowdTraceIsPinned(t *testing.T) {
	if got := randomHistory(t, 5, 1500).Trace(); got != wantCrowdTrace {
		t.Fatalf("crowd trace = %d, want %d", got, wantCrowdTrace)
	}
}

func TestSquadFightTraceIsPinned(t *testing.T) {
	s := build(t, open(20, 10), squadUnits(16, 20, 10))
	for _, u := range s.Units() {
		if u.Team == Scouts {
			s.Command(u.ID, grid.Cell{X: 10, Y: int(u.ID) % 10})
		}
	}
	step(t, s, 1200)
	if got := s.Trace(); got != wantSquadsTrace {
		t.Fatalf("squad fight trace = %d, want %d", got, wantSquadsTrace)
	}
}

// TestOrderHistoryTraceIsPinned records two squads under group orders, an
// attack-move, focused attacks, hold fire, and a guard under orders, across an
// alarm line.
func TestOrderHistoryTraceIsPinned(t *testing.T) {
	rows := open(24, 12)
	rows[4] = "..........####.........."
	rows[7] = "......#####............."
	units := make([]placed, 0, 12)
	for i := range 6 {
		units = append(units, placed{i + 1, "scout", 1, 1 + i*2}, placed{i + 101, "guard", 22, 1 + i*2})
	}
	const alarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 9, "y": 0, "w": 1, "h": 12, "bottom": 0, "top": 32}`
	s := build(t, rows, units, alarm)
	want := func(ok bool) {
		t.Helper()
		if !ok {
			t.Fatalf("tick %d: an order in the recorded history was refused", s.Ticks())
		}
	}
	scouts := []uint64{1, 2, 3, 4, 5, 6}
	for tick := range 1500 {
		switch tick {
		case 0:
			want(s.OrderGroup(scouts[:3], Order{Kind: AttackMove, Goal: grid.Cell{X: 14, Y: 2}}) == 3)
			want(s.OrderGroup(scouts[3:], Order{Kind: Move, Goal: grid.Cell{X: 6, Y: 9}}) == 3)
			want(s.SetHold(6, true))
			want(s.Order(106, Order{Kind: AttackMove, Goal: grid.Cell{X: 14, Y: 11}}))
		case 150:
			want(s.Order(4, Order{Kind: Attack, Target: 106}))
			want(s.OrderGroup(scouts[4:], Order{Kind: Attack, Target: 105}) == 2)
		case 200:
			want(s.SetHold(6, false))
		case 300:
			want(s.OrderGroup(scouts, Order{Kind: AttackMove, Goal: grid.Cell{X: 20, Y: 6}}) > 0)
		}
		step(t, s, 1)
		noOverlap(t, s)
	}
	if !s.Alarm() {
		t.Fatal("no scout crossed the alarm line")
	}
	if got := s.Trace(); got != wantOrdersTrace {
		t.Fatalf("order history trace = %d, want %d", got, wantOrdersTrace)
	}
}

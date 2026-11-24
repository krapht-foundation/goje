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

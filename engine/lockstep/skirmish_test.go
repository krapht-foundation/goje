package lockstep

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/level"
)

// newSkirmish builds the crowded board of the skirmish package's random
// history test with seven scouts and seven guards.
func newSkirmish(tb testing.TB) *skirmish.Sim {
	tb.Helper()
	rows := []string{
		"............",
		"..##....#...",
		"..##....#...",
		"............",
		"....##......",
		"....##..###.",
		"............",
		"............",
	}
	var b strings.Builder
	b.WriteString(`{"version": 1, "rows": ["` + strings.Join(rows, `", "`) + `"], "placements": [`)
	id := 0
	for y, row := range rows {
		for x, c := range row {
			if c != '.' || (x+y)%5 != 0 || id == 14 {
				continue
			}
			kind := "scout"
			if id%2 == 1 {
				kind = "guard"
			}
			if id > 0 {
				b.WriteString(", ")
			}
			id++
			fmt.Fprintf(&b, `{"id": %d, "kind": %q, "x": %d, "y": %d}`, id, kind, x, y)
		}
	}
	b.WriteString("]}")
	board, err := level.Decode(strings.NewReader(b.String()))
	if err != nil {
		tb.Fatal(err)
	}
	s, err := skirmish.New(board)
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

// TestSkirmishSimulationsStayInStep drives two skirmish simulations with
// random orders through lockstep sessions over a lossy-timed link. Each order
// is five payload bytes: unit, kind, goal X, goal Y, and target.
func TestSkirmishSimulationsStayInStep(t *testing.T) {
	for seed := uint64(1); seed <= 4; seed++ {
		net := NewMemoryNet(2)
		net.SetLatency(int(seed))
		net.SetBurst(3)
		cfg := testConfig(2, 0, 6)
		cfg.HashInterval = 30
		ss := sessions(t, net, cfg)
		sims := []*skirmish.Sim{newSkirmish(t), newSkirmish(t)}
		rngs := []*rand.Rand{rand.New(rand.NewPCG(seed, 1)), rand.New(rand.NewPCG(seed, 2))}
		const ticks = 900
		taken := 0
		for round := 0; ss[0].Tick() < ticks || ss[1].Tick() < ticks; round++ {
			if round > 100*ticks {
				t.Fatal("the match did not finish")
			}
			for p, s := range ss {
				if s.Tick() >= ticks {
					continue
				}
				if err := s.Poll(); err != nil {
					t.Fatalf("seed %d player %d: %v", seed, p, err)
				}
				payloads, ok := s.Ready()
				if !ok {
					continue
				}
				for _, b := range payloads {
					for ; len(b) >= 5; b = b[5:] {
						o := skirmish.Order{
							Kind:   skirmish.OrderKind(b[1] % 3),
							Goal:   grid.Cell{X: int(b[2]), Y: int(b[3])},
							Target: uint64(b[4]),
						}
						taken += sims[p].OrderGroup([]uint64{uint64(b[0])}, o)
					}
				}
				if err := sims[p].Step(); err != nil {
					t.Fatal(err)
				}
				if err := s.Done(sims[p].Trace()); err != nil {
					t.Fatalf("seed %d player %d: %v", seed, p, err)
				}
				r := rngs[p]
				if r.IntN(4) == 0 {
					order := []byte{
						byte(r.IntN(14) + 1), byte(r.IntN(3)),
						byte(r.IntN(12)), byte(r.IntN(8)), byte(r.IntN(14) + 1),
					}
					if err := s.Submit(order); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if taken == 0 {
			t.Fatalf("seed %d: no order was taken", seed)
		}
		if sims[0].Trace() != sims[1].Trace() {
			t.Fatalf("seed %d: traces %x and %x differ", seed, sims[0].Trace(), sims[1].Trace())
		}
	}
}

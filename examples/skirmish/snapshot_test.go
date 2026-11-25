package skirmish

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/engine/grid"
)

// orderHistory is a seeded crowd on randomHistory's board, with an alarm line,
// driven by every kind of order. Its input at a tick depends only on the seed
// and the tick, so a run restored from a snapshot can be fed the same input.
type orderHistory struct {
	seed    uint64
	units   []placed
	radius  int  // The sight radius, or 0 to leave vision off.
	versus  bool // Human guards that explore (M17).
	tactics bool // Tactical AI (M17).
}

var historyRows = []string{
	"............",
	"..##....#...",
	"..##....#...",
	"............",
	"....##......",
	"....##..###.",
	"............",
	"............",
}

const historyAlarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 6, "y": 0, "w": 1, "h": 8, "bottom": 0, "top": 32}`

func newOrderHistory(seed uint64) orderHistory {
	free := [][2]int{}
	for y, row := range historyRows {
		for x, c := range row {
			if c == '.' && (x+y)%3 == 0 {
				free = append(free, [2]int{x, y})
			}
		}
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	rng.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
	h := orderHistory{seed: seed}
	for i := range 14 {
		kind := "scout"
		if i%2 == 1 {
			kind = "guard"
		}
		h.units = append(h.units, placed{i + 1, kind, free[i][0], free[i][1]})
	}
	return h
}

func (h orderHistory) build(tb testing.TB) *Sim {
	tb.Helper()
	s := build(tb, historyRows, h.units, historyAlarm)
	if err := s.SetControl(Guards, h.versus); err != nil {
		tb.Fatal(err)
	}
	if err := s.SetTactics(h.tactics); err != nil {
		tb.Fatal(err)
	}
	if h.radius != 0 {
		if err := s.EnableVision(VisionConfig{Radius: h.radius, GuardsExplore: h.versus}); err != nil {
			tb.Fatal(err)
		}
	}
	return s
}

// input gives the orders for one tick.
func (h orderHistory) input(s *Sim, tick int) {
	if tick%5 != 0 {
		return
	}
	rng := rand.New(rand.NewPCG(h.seed, uint64(tick)))
	id := uint64(rng.IntN(14) + 1)
	goal := grid.Cell{X: rng.IntN(12), Y: rng.IntN(8)}
	group := make([]uint64, 0, 4)
	for range 4 {
		group = append(group, uint64(rng.IntN(14)+1))
	}
	switch rng.IntN(7) {
	case 0:
		s.Command(id, goal)
	case 1:
		s.Order(id, Order{Kind: AttackMove, Goal: goal})
	case 2:
		s.Order(id, Order{Kind: Attack, Target: uint64(rng.IntN(14) + 1)})
	case 3:
		s.SetHold(id, rng.IntN(2) == 0)
	case 4:
		s.OrderGroup(group, Order{Kind: AttackMove, Goal: goal})
	case 5:
		s.OrderGroup(group, Order{Kind: Move, Goal: goal})
	case 6:
		s.OrderGroup(group, Order{Kind: Attack, Target: uint64(rng.IntN(14) + 1)})
	}
}

// run feeds ticks [from, to) and checks the board invariants after each.
func (h orderHistory) run(tb testing.TB, s *Sim, from, to int) {
	tb.Helper()
	for tick := from; tick < to; tick++ {
		h.input(s, tick)
		step(tb, s, 1)
		noOverlap(tb, s)
	}
}

// state is everything Restore must reproduce.
type state struct {
	units    []Unit
	bolts    []Bolt
	alarm    bool
	tick     uint64
	trace    uint64
	boltSeq  uint64
	overlaps []collide.Overlap
	explored [2][]uint64
	visible  []bool // Each cell, seen by the scouts and then by the guards.
}

func stateOf(s *Sim) state {
	units := slices.Clone(s.units)
	for i := range units {
		units[i].route = slices.Clone(units[i].route)
	}
	visible := make([]bool, 0, 2*s.width*s.height)
	for _, team := range []Team{Scouts, Guards} {
		for c := range s.width * s.height {
			visible = append(visible, s.Visible(team, grid.Cell{X: c % s.width, Y: c / s.width}))
		}
	}
	return state{
		units,
		slices.Clone(s.bolts),
		s.alarm,
		s.tick,
		s.trace,
		s.boltSeq,
		s.tracker.AppendOverlaps(nil),
		[2][]uint64{slices.Clone(s.explored), slices.Clone(s.guardExplored)},
		visible,
	}
}

func TestRestoreThenContinueEqualsContinue(t *testing.T) {
	const total = 900
	sawBolts, sawInside := false, false
	for seed := uint64(1); seed <= 6; seed++ {
		h := newOrderHistory(seed)
		for _, at := range []int{1, 60, 240, 480, 700} {
			original := h.build(t)
			h.run(t, original, 0, at)
			snap, err := original.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			sawBolts = sawBolts || len(original.Bolts()) > 0
			sawInside = sawInside || len(original.tracker.AppendOverlaps(nil)) > 0

			restored := h.build(t)
			if err := restored.Restore(snap); err != nil {
				t.Fatalf("seed %d tick %d: %v", seed, at, err)
			}
			if again, _ := restored.Snapshot(); !bytes.Equal(again, snap) {
				t.Fatalf("seed %d tick %d: a restored Sim snapshots differently", seed, at)
			}
			h.run(t, original, at, total)
			h.run(t, restored, at, total)
			if a, b := stateOf(original), stateOf(restored); !reflect.DeepEqual(a, b) {
				t.Fatalf("seed %d tick %d: restored run ended in another state: traces %d and %d",
					seed, at, a.trace, b.trace)
			}
		}
	}
	if !sawBolts || !sawInside {
		t.Fatalf(
			"no snapshot caught bolts in flight (%v) or a scout inside the alarm line (%v)",
			sawBolts,
			sawInside,
		)
	}
}

func TestRestoreRejectsDamagedSnapshotsAndKeepsTheSim(t *testing.T) {
	// Every unit is idle and every route empty, so unit i starts at 33+46*i.
	s := build(
		t,
		[]string{".....", "..#..", "....."},
		[]placed{{1, "scout", 0, 0}, {2, "guard", 4, 2}, {3, "scout", 0, 2}},
	)
	snap, _ := s.Snapshot()
	const header, record = 33, 46
	put32 := func(b []byte, at int, v uint32) []byte {
		b = slices.Clone(b)
		binary.LittleEndian.PutUint32(b[at:], v)
		return b
	}
	put8 := func(b []byte, at int, v byte) []byte {
		b = slices.Clone(b)
		b[at] = v
		return b
	}
	put64 := func(b []byte, at int, v uint64) []byte {
		b = slices.Clone(b)
		binary.LittleEndian.PutUint64(b[at:], v)
		return b
	}
	cases := map[string][]byte{
		"version":          put8(snap, 0, 2),
		"trailing data":    append(slices.Clone(snap), 0),
		"other board":      put8(snap, 2, 6),
		"unit ID":          put8(snap, header, 9),
		"health over max":  put32(snap, header+16, scoutHP+1),
		"living with none": put32(snap, header+16, 0),
		"unknown flag":     put8(snap, header+20, 0x10),
		"bad order":        put8(snap, header+21, 3),
		"attack a friend":  put64(put8(snap, header+21, byte(Attack)), header+22, 3),
		"overlap a unit": put32(
			put32(snap, header+2*record+8, uint32(fixed.One/2-BodySize/2)),
			header+2*record+12,
			uint32(fixed.One/2-BodySize/2),
		),
		"inside a wall": put32(
			put32(snap, header+8, uint32(2*fixed.One+80)),
			header+12,
			uint32(fixed.One+80),
		),
		"outside the board":  put32(snap, header+8, uint32(5*fixed.One-BodySize+1)),
		"cell off the board": put8(put8(snap, header+30, 15), header+31, 0),
		"route longer":       put8(snap, header+44, 1),
		"cooldown":           put8(snap, header+41, weaponCooldown+1),
		"bolt count":         put8(snap, header+3*record, 1),
		"alarm":              put8(snap, 32, 2),
	}
	for name, bad := range cases {
		if err := s.Restore(bad); err == nil {
			t.Errorf("%s: Restore accepted the snapshot", name)
		}
	}
	for n := range len(snap) {
		if err := s.Restore(snap[:n]); err == nil {
			t.Fatalf("a snapshot cut to %d of %d bytes was accepted", n, len(snap))
		}
	}
	if after, _ := s.Snapshot(); !bytes.Equal(after, snap) {
		t.Fatal("a rejected Restore changed the Sim")
	}
	// The friendly attack was rejected only for its target: an enemy is fine.
	if err := s.Restore(put64(put8(snap, header+21, byte(Attack)), header+22, 2)); err != nil {
		t.Fatalf("an attack on an enemy was rejected: %v", err)
	}
}

func TestRandomOrderHistoriesReplayAndTheirOrdersEnd(t *testing.T) {
	for seed := uint64(1); seed <= 8; seed++ {
		h := newOrderHistory(seed)
		first, second := h.build(t), h.build(t)
		h.run(t, first, 0, 1200)
		h.run(t, second, 0, 1200)
		if first.Trace() != second.Trace() {
			t.Fatalf("seed %d: two runs of one history gave different traces", seed)
		}
		// With no more input, every order ends, except an attack whose target lives
		// and an attack-move that waits beside an enemy it may be unable to hit
		// through a wall, because range ignores walls.
		for _, u := range first.Units() {
			first.SetHold(u.ID, false)
		}
		step(t, first, 3000)
		for i := range first.units {
			u := &first.units[i]
			switch {
			case !u.Alive:
			case u.order == Attack:
				if !unit(t, first, u.target).Alive {
					t.Fatalf("seed %d: unit %d still attacks dead unit %d", seed, u.ID, u.target)
				}
			case u.order == AttackMove:
				if _, _, ok := first.nearest(u, weaponRange, true, false); !ok {
					t.Fatalf("seed %d: unit %d never finished its attack-move", seed, u.ID)
				}
			case u.Moving() && !u.chasing:
				t.Fatalf("seed %d: unit %d never finished its move", seed, u.ID)
			}
		}
	}
}

func TestOrdersDoNotAllocate(t *testing.T) {
	h := newOrderHistory(3)
	s := h.build(t)
	h.run(t, s, 0, 300)
	ids := make([]uint64, 0, MaxUnits)
	assigned := make([]Assignment, 0, MaxUnits)
	tick := 300
	allocs := testing.AllocsPerRun(300, func() {
		h.inputNoAlloc(s, tick, ids)
		ids = ids[:0]
		for i := range s.Units() {
			if u := &s.Units()[i]; u.Alive && u.Team == Scouts {
				ids = append(ids, u.ID)
			}
		}
		assigned = s.AssignGoals(assigned[:0], ids, grid.Cell{X: 6, Y: 3})
		if err := s.Step(); err != nil {
			t.Fatal(err)
		}
		tick++
	})
	if allocs != 0 {
		t.Fatalf("a tick with orders allocated %v times", allocs)
	}
}

// inputNoAlloc issues an order of every kind without building a group slice.
func (h orderHistory) inputNoAlloc(s *Sim, tick int, group []uint64) {
	id := uint64(tick%14 + 1)
	goal := grid.Cell{X: tick % 12, Y: tick % 8}
	switch tick % 6 {
	case 0:
		s.Order(id, Order{Kind: AttackMove, Goal: goal})
	case 1:
		s.Order(id, Order{Kind: Attack, Target: uint64((tick/6)%14 + 1)})
	case 2:
		s.SetHold(id, tick%4 == 2)
	case 3:
		s.OrderGroup(group, Order{Kind: AttackMove, Goal: goal})
	case 4:
		s.OrderGroup(group, Order{Kind: Attack, Target: uint64((tick/6)%14 + 1)})
	case 5:
		s.Command(id, goal)
	}
}

func FuzzRestore(f *testing.F) {
	h := newOrderHistory(2)
	for _, at := range []int{0, 100, 400} {
		s := h.build(f)
		h.run(f, s, 0, at)
		snap, _ := s.Snapshot()
		f.Add(snap)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s := h.build(t)
		before, _ := s.Snapshot()
		if err := s.Restore(data); err != nil {
			if after, _ := s.Snapshot(); !bytes.Equal(before, after) {
				t.Fatal("a rejected Restore changed the Sim")
			}
			return
		}
		// Whatever Restore accepts must play without breaking a rule.
		for range 60 {
			if err := s.Step(); err != nil {
				t.Fatalf("tick %d after Restore: %v", s.Ticks(), err)
			}
			noOverlap(t, s)
		}
	})
}

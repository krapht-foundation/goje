package skirmish

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"reflect"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/engine/vision"
)

// withVision turns vision on for a Sim built by build.
func withVision(tb testing.TB, s *Sim, radius int) *Sim {
	tb.Helper()
	if err := s.EnableVision(VisionConfig{Radius: radius}); err != nil {
		tb.Fatal(err)
	}
	return s
}

func fired(s *Sim) int {
	n := 0
	for _, e := range s.Events() {
		if e.Kind == EventBoltFired {
			n++
		}
	}
	return n
}

func TestEnableVisionRules(t *testing.T) {
	s := build(t, open(5, 5), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 4}})
	for _, c := range []grid.Cell{{X: 0, Y: 0}, {X: 4, Y: 4}} {
		if !s.Visible(Scouts, c) || !s.Visible(Guards, c) || !s.Explored(c) {
			t.Fatalf("without vision, cell %v is hidden", c)
		}
	}
	if s.Visible(Scouts, grid.Cell{X: 5, Y: 0}) || s.Explored(grid.Cell{X: -1, Y: 0}) ||
		s.Visible(0, grid.Cell{}) {
		t.Fatal("a cell off the board, or a team that does not exist, sees something")
	}
	for _, r := range []int{0, -1, vision.MaxRadius + 1} {
		if s.EnableVision(VisionConfig{Radius: r}) == nil {
			t.Fatalf("radius %d was accepted", r)
		}
	}
	withVision(t, s, 2)
	if s.EnableVision(VisionConfig{Radius: 2}) == nil {
		t.Fatal("vision was enabled twice")
	}
	if !s.Visible(Scouts, grid.Cell{X: 2, Y: 0}) || s.Visible(Scouts, grid.Cell{X: 3, Y: 0}) ||
		!s.Explored(grid.Cell{X: 2, Y: 0}) || s.Explored(grid.Cell{X: 4, Y: 4}) {
		t.Fatal("the scout's sight or explored cells are wrong at the start")
	}
	if !s.Visible(Guards, grid.Cell{X: 4, Y: 4}) || s.Visible(Guards, grid.Cell{X: 0, Y: 0}) {
		t.Fatal("the guard's sight is wrong at the start")
	}
	later := build(t, open(5, 5), []placed{{1, "scout", 0, 0}})
	step(t, later, 1)
	if later.EnableVision(VisionConfig{Radius: 2}) == nil {
		t.Fatal("vision was enabled after a Step")
	}
}

func TestOnlyWallsBlockSight(t *testing.T) {
	s := withVision(t, build(t, []string{"..#.."}, []placed{{1, "scout", 0, 0}}), 7)
	if !s.Visible(Scouts, grid.Cell{X: 2, Y: 0}) || s.Visible(Scouts, grid.Cell{X: 3, Y: 0}) {
		t.Fatal("a wall should be seen and hide what is behind it")
	}
	// Neither a raised deck nor a solid zone that blocks units hides anything.
	for _, zone := range []string{
		`{"id": 1, "kind": "deck", "role": "solid", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 24, "top": 32}`,
		`{"id": 1, "kind": "crate", "role": "solid", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 32}`,
	} {
		s := withVision(t, build(t, open(5, 1), []placed{{1, "scout", 0, 0}}, zone), 7)
		if !s.Visible(Scouts, grid.Cell{X: 4, Y: 0}) {
			t.Fatalf("a zone blocked sight: %s", zone)
		}
	}
}

func TestNothingFiresAtAHiddenEnemyAndHiddenScoutsWakeNoGuard(t *testing.T) {
	// Without vision, both units fire at each other through the wall, and the
	// guard, within six tiles, wakes.
	for _, radius := range []int{0, 7} {
		s := build(t, []string{"..#...."}, []placed{{1, "scout", 1, 0}, {2, "guard", 5, 0}})
		if radius != 0 {
			withVision(t, s, radius)
		}
		shots := 0
		for range 60 {
			step(t, s, 1)
			shots += fired(s)
		}
		if hidden := radius != 0; (shots == 0) != hidden || unit(t, s, 2).chasing == hidden {
			t.Fatalf("radius %d: %d shots, guard chasing %v", radius, shots, unit(t, s, 2).chasing)
		}
	}
}

func TestGuardsWakeWhenTheirTeamSeesAScout(t *testing.T) {
	// Guard 3 sees the scout; guard 2, ten tiles away, does not, but its team does.
	units := []placed{{1, "scout", 0, 0}, {2, "guard", 10, 0}, {3, "guard", 2, 2}}
	off := build(t, open(20, 3), units)
	step(t, off, 1)
	if unit(t, off, 2).chasing {
		t.Fatal("without vision, a guard woke for a scout beyond six tiles")
	}
	on := withVision(t, build(t, open(20, 3), units), 3)
	if on.Visible(Guards, grid.Cell{X: 0, Y: 0}) != true ||
		on.Visible(Scouts, grid.Cell{X: 10, Y: 0}) {
		t.Fatal("the setup's sight is wrong")
	}
	step(t, on, 1)
	if !unit(t, on, 2).chasing || !unit(t, on, 3).chasing {
		t.Fatal("with vision, guards did not wake when their team saw a scout")
	}
}

func TestAlarmedGuardsHuntWithoutSight(t *testing.T) {
	const alarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 32}`
	s := withVision(
		t,
		build(t, open(30, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 29, 0}}, alarm),
		3,
	)
	s.Command(1, grid.Cell{X: 3, Y: 0})
	step(t, s, 100)
	g := unit(t, s, 2)
	if !s.Alarm() || !g.chasing || g.Center().X >= fixed.FromInt(29) {
		t.Fatalf("an alarmed guard did not hunt: alarm %v, chasing %v", s.Alarm(), g.chasing)
	}
}

func TestAttackNeedsSightAndEndsWhereTheTargetWasLastSeen(t *testing.T) {
	units := []placed{{1, "scout", 0, 1}, {2, "guard", 3, 1}, {3, "guard", 8, 1}}
	off := build(t, open(16, 3), units)
	if !off.Order(1, Order{Kind: Attack, Target: 3}) {
		t.Fatal("without vision, an attack on a reachable guard was refused")
	}
	s := withVision(t, build(t, open(16, 3), units), 3)
	if s.Order(1, Order{Kind: Attack, Target: 3}) {
		t.Fatal("an attack on a guard out of sight was accepted")
	}
	// The guard walks away without stopping or shooting, and the scout, which
	// holds its fire, follows until it loses sight of it.
	if !s.SetHold(1, true) || !s.SetHold(2, true) ||
		!s.Order(2, Order{Kind: AttackMove, Goal: grid.Cell{X: 15, Y: 1}}) ||
		!s.Order(1, Order{Kind: Attack, Target: 2}) {
		t.Fatal("the setup's orders were refused")
	}
	scout, guard := unit(t, s, 1), unit(t, s, 2)
	last, lost := -1, false
	for range 600 {
		step(t, s, 1)
		if c := s.cellOf(guard.Center()); s.Visible(Scouts, grid.Cell{X: c % 16, Y: c / 16}) {
			last = c
		} else {
			lost = true
		}
		if scout.order != Attack {
			break
		}
	}
	if !lost || scout.order == Attack || scout.Moving() || s.offCenter(scout) ||
		s.cellOf(scout.Center()) != last {
		t.Fatalf("the attack did not end on the last seen cell %d: order %d at cell %d",
			last, scout.order, s.cellOf(scout.Center()))
	}
	if s.Order(1, Order{Kind: Attack, Target: 2}) {
		t.Fatal("an attack on a guard that left sight was accepted")
	}
}

func TestAttackMoveDoesNotStopForAHiddenEnemy(t *testing.T) {
	rows := []string{"............", "############", "............"}
	s := withVision(t, build(t, rows, []placed{{1, "scout", 0, 0}, {2, "guard", 5, 2}}), 7)
	if !s.Order(1, Order{Kind: AttackMove, Goal: grid.Cell{X: 11, Y: 0}}) {
		t.Fatal("order refused")
	}
	shots := 0
	for range 200 {
		step(t, s, 1)
		shots += fired(s)
	}
	if scout := unit(t, s, 1); scout.order != Move || scout.Moving() || shots != 0 ||
		cellOfUnit(s, scout) != (grid.Cell{X: 11, Y: 0}) {
		t.Fatalf("the attack-move stopped or fired: at %v, %d shots", cellOfUnit(s, scout), shots)
	}
}

// visibility returns which cells each team sees, scouts first.
func visibility(s *Sim) [2][]bool {
	var v [2][]bool
	for i, team := range []Team{Scouts, Guards} {
		for c := range s.width * s.height {
			v[i] = append(v[i], s.Visible(team, grid.Cell{X: c % s.width, Y: c / s.width}))
		}
	}
	return v
}

// checkShots fails if a bolt fired in the last Step was aimed at an enemy
// that the shooter's team did not see. Sight during a tick is what it was
// after the previous one; the shooter and the target stand where they are now.
func checkShots(tb testing.TB, s *Sim, before [2][]bool, seq uint64) {
	tb.Helper()
	for _, e := range s.Events() {
		if e.Kind != EventBoltFired {
			continue
		}
		seq++
		u, _ := s.Unit(e.Unit)
		var velocity fixed.Vec2
		for _, b := range s.bolts {
			if b.ID == boltIDBase+seq {
				velocity = b.Velocity
			}
		}
		ok := false
		for i := range s.units {
			target := &s.units[i]
			if target.Team == u.Team || !inRange(u, target) {
				continue
			}
			aim := target.Center().Sub(e.At)
			if velocity != (fixed.Vec2{}) && aim.WithLength(boltSpeed) != velocity {
				continue
			}
			ok = ok || before[u.Team-1][s.cellOf(target.Center())]
		}
		if !ok {
			tb.Fatalf("tick %d: unit %d fired at an enemy its team did not see", s.Ticks(), u.ID)
		}
	}
}

func explored(s *Sim) int {
	n := 0
	for _, w := range s.explored {
		n += bits.OnesCount64(w)
	}
	return n
}

// TestRandomVisionHistories runs seeded order histories with vision on and
// checks after every tick that nobody fired at a hidden enemy and that the
// explored cells only grow and include what the scouts see. The histories
// replay identically, and once the input stops every order ends.
func TestRandomVisionHistories(t *testing.T) {
	for seed := uint64(1); seed <= 8; seed++ {
		h := newOrderHistory(seed)
		h.radius = 4
		first, second := h.build(t), h.build(t)
		was := explored(first)
		for tick := range 1200 {
			before, seq := visibility(first), first.boltSeq
			h.run(t, first, tick, tick+1)
			checkShots(t, first, before, seq)
			now := visibility(first)
			for c, seen := range now[0] {
				if seen && !first.Explored(grid.Cell{X: c % first.width, Y: c / first.width}) {
					t.Fatalf("seed %d tick %d: a cell in sight is not explored", seed, tick)
				}
			}
			if n := explored(first); n < was {
				t.Fatalf("seed %d tick %d: explored cells shrank", seed, tick)
			} else {
				was = n
			}
		}
		h.run(t, second, 0, 1200)
		if first.Trace() != second.Trace() {
			t.Fatalf("seed %d: two runs of one history gave different traces", seed)
		}
		for _, u := range first.Units() {
			first.SetHold(u.ID, false)
		}
		step(t, first, 3000)
		for i := range first.units {
			u := &first.units[i]
			switch {
			case !u.Alive:
			case u.order == Attack:
				if tg := unit(t, first, u.target); !tg.Alive || !first.sees(u, tg) {
					t.Fatalf("seed %d: unit %d still attacks unit %d, dead or hidden", seed, u.ID, u.target)
				}
			case u.order == AttackMove:
				if _, _, ok := first.nearest(u, weaponRange, true); !ok {
					t.Fatalf("seed %d: unit %d never finished its attack-move", seed, u.ID)
				}
			case u.Moving() && !u.chasing:
				t.Fatalf("seed %d: unit %d never finished its move", seed, u.ID)
			}
		}
	}
}

func TestStepWithVisionDoesNotAllocate(t *testing.T) {
	h := newOrderHistory(3)
	h.radius = 5
	s := h.build(t)
	h.run(t, s, 0, 300)
	allocs := testing.AllocsPerRun(200, func() {
		if err := s.Step(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Step with vision allocated %v times per tick", allocs)
	}
}

func TestRestoreWithVisionThenContinueEqualsContinue(t *testing.T) {
	const total = 900
	for seed := uint64(1); seed <= 4; seed++ {
		h := newOrderHistory(seed)
		h.radius = 4
		for _, at := range []int{1, 60, 240, 480, 700} {
			original := h.build(t)
			h.run(t, original, 0, at)
			snap, err := original.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			restored := h.build(t)
			if err := restored.Restore(snap); err != nil {
				t.Fatalf("seed %d tick %d: %v", seed, at, err)
			}
			if a, b := stateOf(original), stateOf(restored); !reflect.DeepEqual(a, b) {
				t.Fatalf("seed %d tick %d: a restored Sim differs before it runs", seed, at)
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
}

func TestRestoreRejectsMismatchedVisionSnapshots(t *testing.T) {
	rows, units := []string{
		".....",
		"..#..",
		".....",
	}, []placed{
		{1, "scout", 0, 0},
		{2, "guard", 4, 2},
	}
	plain := build(t, rows, units)
	s := withVision(t, build(t, rows, units), 3)
	old, _ := plain.Snapshot()
	snap, _ := s.Snapshot()
	// The vision section follows the M14 layout: the radius, two bytes of
	// explored cells for 15 cells, and a last-seen cell for each unit.
	at := len(old)
	if len(snap) != at+1+2+4*2 {
		t.Fatalf("a vision snapshot is %d bytes, want %d", len(snap), at+11)
	}
	set := func(i int, v ...byte) []byte {
		b := slices.Clone(snap)
		copy(b[i:], v)
		return b
	}
	cases := map[string][]byte{
		"a snapshot without vision": old,
		"another radius":            set(at, 4),
		"explored off the board":    set(at+2, 0x80),
		"last seen off the board":   set(at+3, 15, 0, 0, 0),
		"last seen below -1": binary.LittleEndian.AppendUint32(
			slices.Clone(snap[:at+7]),
			0xfffffffe,
		),
	}
	for name, bad := range cases {
		if err := s.Restore(bad); err == nil {
			t.Errorf("%s: Restore accepted the snapshot", name)
		}
	}
	for n := range len(snap) {
		if s.Restore(snap[:n]) == nil {
			t.Fatalf("a snapshot cut to %d of %d bytes was accepted", n, len(snap))
		}
	}
	if err := plain.Restore(snap); err == nil {
		t.Error("a Sim without vision accepted a vision snapshot")
	}
	if after, _ := s.Snapshot(); !bytes.Equal(after, snap) {
		t.Fatal("a rejected Restore changed the Sim")
	}
}

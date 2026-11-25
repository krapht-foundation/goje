package skirmish

import (
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

// withTactics turns tactics on for a Sim built by build.
func withTactics(tb testing.TB, s *Sim) *Sim {
	tb.Helper()
	if err := s.SetTactics(true); err != nil {
		tb.Fatal(err)
	}
	return s
}

// firstHit steps until shooter fires and returns whom the next hit struck.
func firstHit(t *testing.T, s *Sim, shooter uint64) uint64 {
	t.Helper()
	fired := false
	for range 100 {
		step(t, s, 1)
		for _, e := range s.Events() {
			switch {
			case e.Kind == EventBoltFired && e.Unit == shooter:
				fired = true
			case e.Kind == EventBoltHit && fired:
				return e.Unit
			}
		}
	}
	t.Fatal("the guard's bolt hit nobody")
	return 0
}

// TestTacticalGuardsFocusTheWeakestScout puts a hurt scout behind a healthy
// one. A classic guard shoots the nearest; a tactical guard the weakest.
func TestTacticalGuardsFocusTheWeakestScout(t *testing.T) {
	for _, tactics := range []bool{false, true} {
		s := build(t, open(5, 3), []placed{{1, "scout", 2, 0}, {2, "scout", 1, 2}, {9, "guard", 4, 0}})
		if err := s.SetTactics(tactics); err != nil {
			t.Fatal(err)
		}
		s.SetHold(1, true)
		s.SetHold(2, true)
		unit(t, s, 2).HP = 20
		want := uint64(1)
		if tactics {
			want = 2
		}
		if got := firstHit(t, s, 9); got != want {
			t.Fatalf("tactics %v: the guard hit unit %d, want %d", tactics, got, want)
		}
	}
}

// TestHurtGuardWakesItsNeighbors has a scout shoot a guard while another
// guard stands within the wake radius of the first but beyond its own sight
// of the scout.
func TestHurtGuardWakesItsNeighbors(t *testing.T) {
	for _, tactics := range []bool{false, true} {
		s := build(t, open(20, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 3, 0}, {3, "guard", 8, 0}})
		if err := s.SetTactics(tactics); err != nil {
			t.Fatal(err)
		}
		woke := false
		for range 60 {
			step(t, s, 1)
			if g := unit(t, s, 3); g.chasing {
				woke = true
				break
			}
		}
		if woke != tactics {
			t.Fatalf("tactics %v: the far guard woke %v", tactics, woke)
		}
	}
}

// TestGuardOneHitFromDeathFallsBack checks that a tactical guard with one
// hit left walks out of weapon range while an ally fights, and fights on
// alone.
func TestGuardOneHitFromDeathFallsBack(t *testing.T) {
	for _, c := range []struct {
		tactics, ally, wantBack bool
	}{{false, true, false}, {true, true, true}, {true, false, false}} {
		// The ally has the lower ID, so it is hunting when the hurt guard decides.
		units := []placed{{1, "scout", 0, 1}, {3, "guard", 3, 1}}
		if c.ally {
			units = append(units, placed{2, "guard", 3, 0})
		}
		s := build(t, open(20, 3), units)
		if err := s.SetTactics(c.tactics); err != nil {
			t.Fatal(err)
		}
		s.SetHold(1, true)
		unit(t, s, 3).HP = weaponDamage
		step(t, s, 1)
		g, scout := unit(t, s, 3), unit(t, s, 1)
		goal, moving := s.Goal(g)
		back := moving && !s.covers(goal.Y*s.width+goal.X, scout)
		if back != c.wantBack {
			t.Fatalf("%+v: fell back %v (goal %v)", c, back, goal)
		}
		if !c.wantBack {
			continue
		}
		step(t, s, 100)
		if inRange(g, scout) || g.Moving() {
			t.Fatalf("%+v: the guard is still in range or walking", c)
		}
	}
}

// TestEverySecondGuardFlanks has three guards hunt one scout. The middle one
// by ID walks toward a waypoint three cells to the side of the line between
// it and the scout, which here brings it into range on the scout's flank;
// the others go straight.
func TestEverySecondGuardFlanks(t *testing.T) {
	s := withTactics(t, build(t, open(20, 11), []placed{
		{1, "scout", 0, 5}, {101, "guard", 5, 4}, {102, "guard", 5, 5}, {103, "guard", 5, 6},
	}))
	s.SetHold(1, true)
	unit(t, s, 1).HP = 1000 // The scout outlives the flank.
	step(t, s, 1)
	want := 8*s.width + 2 // Midpoint (2, 5), three cells down.
	if a, b, c := unit(
		t,
		s,
		101,
	).waypoint, unit(
		t,
		s,
		102,
	).waypoint, unit(
		t,
		s,
		103,
	).waypoint; a != noWaypoint || b != want ||
		c != noWaypoint {
		t.Fatalf("waypoints %d, %d, %d; want %d, %d, %d", a, b, c, noWaypoint, want, noWaypoint)
	}
	side := false
	for range 400 {
		step(t, s, 1)
		g := unit(t, s, 102)
		side = side || cellOfUnit(s, g).Y == 8
		if g.waypoint == flankDone {
			break
		}
	}
	if g := unit(t, s, 102); !side || g.waypoint != flankDone || !inRange(g, unit(t, s, 1)) {
		t.Fatalf("the flanker did not reach the scout's flank: row 8 %v, waypoint %d", side, g.waypoint)
	}
}

// TestWaypointAvoidsSolidCells puts a wall where the first side's waypoint
// would be, so the flanker takes the other side.
func TestWaypointAvoidsSolidCells(t *testing.T) {
	rows := open(20, 11)
	rows[8] = "..#................."
	s := withTactics(t, build(t, rows, []placed{
		{1, "scout", 0, 5}, {101, "guard", 5, 4}, {102, "guard", 5, 5}, {103, "guard", 5, 6},
	}))
	s.SetHold(1, true)
	step(t, s, 1)
	if got, want := unit(t, s, 102).waypoint, 2*s.width+2; got != want {
		t.Fatalf("waypoint %d, want %d", got, want)
	}
}

// wantTacticsTrace pins a recorded battle with tactics on: eight scouts
// attack-move into eight tactical guards around two walls. Native and
// WebAssembly runs must agree.
const wantTacticsTrace uint64 = 7400374754257848278

func TestTacticsTraceIsPinned(t *testing.T) {
	rows := open(24, 12)
	rows[3] = "..........###..........."
	rows[8] = "...........###.........."
	units := make([]placed, 0, 16)
	for i := range 8 {
		units = append(
			units,
			placed{i + 1, "scout", 1 + i%2, 2 + i},
			placed{i + 101, "guard", 21 - i%2, 2 + i},
		)
	}
	s := withTactics(t, build(t, rows, units))
	scouts := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	flanked, fellBack := false, false
	for tick := range 1500 {
		if tick%300 == 0 {
			s.OrderGroup(scouts, Order{Kind: AttackMove, Goal: grid.Cell{X: 20, Y: 6}})
		}
		step(t, s, 1)
		noOverlap(t, s)
		for i := range s.units {
			u := &s.units[i]
			flanked = flanked || u.waypoint >= 0
			if goal, ok := s.Goal(u); ok && u.Team == Guards && u.HP <= weaponDamage && u.chasing {
				for j := range s.units {
					if sc := &s.units[j]; sc.Alive && sc.Team == Scouts && inRange(u, sc) &&
						!s.covers(goal.Y*s.width+goal.X, sc) {
						fellBack = true
					}
				}
			}
		}
	}
	if !flanked || !fellBack {
		t.Fatalf("the battle did not exercise tactics: flanked %v, fell back %v", flanked, fellBack)
	}
	if got := s.Trace(); got != wantTacticsTrace {
		t.Fatalf("tactics trace = %d, want %d", got, wantTacticsTrace)
	}
}

func TestStepWithTacticsDoesNotAllocate(t *testing.T) {
	h := newOrderHistory(3)
	h.radius, h.tactics = 5, true
	s := h.build(t)
	h.run(t, s, 0, 300)
	allocs := testing.AllocsPerRun(200, func() {
		if err := s.Step(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Step with tactics allocated %v times per tick", allocs)
	}
}

// BenchmarkTacticsStep measures a tick of a 64 against 64 battle with
// tactics on, for comparison with BenchmarkClassicStep. A decided battle
// starts over, so every measured tick has a fight.
func BenchmarkTacticsStep(b *testing.B) { benchmarkSquads(b, true) }

// BenchmarkClassicStep is BenchmarkTacticsStep with tactics off.
func BenchmarkClassicStep(b *testing.B) { benchmarkSquads(b, false) }

func benchmarkSquads(b *testing.B, tactics bool) {
	b.Helper()
	battle := func() *Sim {
		s := build(b, open(32, 32), squadUnits(64, 32, 32))
		if err := s.SetTactics(tactics); err != nil {
			b.Fatal(err)
		}
		for _, u := range s.Units() {
			if u.Team == Scouts {
				s.Command(u.ID, grid.Cell{X: 16, Y: int(u.ID) % 32})
			}
		}
		return s
	}
	s := battle()
	b.ReportAllocs()
	for b.Loop() {
		if err := s.Step(); err != nil {
			b.Fatal(err)
		}
		if _, over := s.Winner(); over || s.Ticks() == 3000 {
			b.StopTimer()
			s = battle()
			b.StartTimer()
		}
	}
}

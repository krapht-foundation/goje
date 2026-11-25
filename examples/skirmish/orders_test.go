package skirmish

import (
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

func TestOrderRules(t *testing.T) {
	s := build(t, []string{"......", "....#.", "...#.."}, []placed{
		{
			1,
			"scout",
			0,
			0,
		},
		{2, "scout", 1, 0},
		{3, "guard", 5, 0},
		{4, "guard", 5, 2},
		{5, "scout", 5, 1},
	})
	tests := []struct {
		name string
		id   uint64
		o    Order
		want bool
	}{
		{"unknown unit", 9, Order{Kind: AttackMove, Goal: grid.Cell{X: 3, Y: 0}}, false},
		{"unknown kind", 1, Order{Kind: AttackMove + 1, Goal: grid.Cell{X: 3, Y: 0}}, false},
		{"attack-move off the board", 1, Order{Kind: AttackMove, Goal: grid.Cell{X: 6, Y: 0}}, false},
		{
			"attack-move onto an idle unit",
			1,
			Order{Kind: AttackMove, Goal: grid.Cell{X: 1, Y: 0}},
			false,
		},
		{"attack an unknown unit", 1, Order{Kind: Attack, Target: 9}, false},
		{"attack a friend", 1, Order{Kind: Attack, Target: 2}, false},
		{"attack itself", 1, Order{Kind: Attack, Target: 1}, false},
		// Guard 4 is walled in at 5, 2 by the wall and scout 5, and is out of range.
		{"attack an unreachable enemy", 1, Order{Kind: Attack, Target: 4}, false},
		{"attack a reachable enemy", 1, Order{Kind: Attack, Target: 3}, true},
		{"attack-move to a free cell", 2, Order{Kind: AttackMove, Goal: grid.Cell{X: 0, Y: 2}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.Order(tc.id, tc.o); got != tc.want {
				t.Fatalf("Order = %v, want %v", got, tc.want)
			}
		})
	}
	if u := unit(t, s, 1); u.order != Attack || u.target != 3 || !u.Moving() {
		t.Fatalf(
			"unit 1 has order %d on %d, moving %v; want an attack on 3 under way",
			u.order,
			u.target,
			u.Moving(),
		)
	}
	// A refused order keeps the attack.
	if s.Order(1, Order{Kind: Move, Goal: grid.Cell{X: 4, Y: 1}}) || unit(t, s, 1).order != Attack {
		t.Fatal("a refused move replaced the attack")
	}
	if !s.Command(1, grid.Cell{X: 2, Y: 1}) || unit(t, s, 1).order != Move ||
		unit(t, s, 1).target != 0 {
		t.Fatal("Command did not replace the attack with a move")
	}
}

func TestMoveOrderIsCommand(t *testing.T) {
	a, b := randomHistory(t, 4, 50), randomHistory(t, 4, 50)
	for id := uint64(1); id <= 14; id++ {
		goal := grid.Cell{X: int(id) % 12, Y: int(id) % 8}
		if a.Command(id, goal) != b.Order(id, Order{Kind: Move, Goal: goal}) {
			t.Fatalf("unit %d: Command and Order disagree", id)
		}
	}
	step(t, a, 300)
	step(t, b, 300)
	if a.Trace() != b.Trace() {
		t.Fatal("a Move order and a command led to different traces")
	}
}

func TestAttackShootsOnlyItsTarget(t *testing.T) {
	// Guard 2 is nearer, so an ordinary scout shoots it first; the attack on guard 3 overrides that.
	s := build(t, open(7, 1), []placed{{1, "scout", 3, 0}, {2, "guard", 1, 0}, {3, "guard", 6, 0}})
	if !s.Order(1, Order{Kind: Attack, Target: 3}) {
		t.Fatal("attack refused")
	}
	step(t, s, 15)
	if near, far := unit(
		t,
		s,
		2,
	).HP, unit(
		t,
		s,
		3,
	).HP; near != guardHP ||
		far != guardHP-weaponDamage {
		t.Fatalf("guard 2 has %d health and guard 3 has %d; want %d and %d",
			near, far, guardHP, guardHP-weaponDamage)
	}
	if u := unit(t, s, 1); u.Moving() || u.order != Attack {
		t.Fatal("a unit in range of its target walked or dropped the attack")
	}
}

func TestAttackRoutesAgainEveryChasePeriodAndEndsWithTheTarget(t *testing.T) {
	// Guard 9 attacks scout 1, which walks away along the corridor and holds its
	// fire. Guards on an attack order do not run the guard AI.
	s := build(t, open(40, 2), []placed{{1, "scout", 12, 0}, {9, "guard", 0, 1}})
	if !s.Command(1, grid.Cell{X: 39, Y: 0}) || !s.SetHold(1, true) ||
		!s.Order(9, Order{Kind: Attack, Target: 1}) {
		t.Fatal("orders refused")
	}
	guard := unit(t, s, 9)
	last, _ := s.Goal(guard)
	changes := 0
	for range 120 {
		step(t, s, 1)
		goal, ok := s.Goal(guard)
		if ok && goal != last {
			if (s.Ticks()+guard.ID)%chasePeriod != 0 {
				t.Fatalf("tick %d: the attacker routed again off its period", s.Ticks())
			}
			changes++
		}
		last = goal
	}
	if changes < 3 {
		t.Fatalf("the attacker routed again %d times in 120 ticks, want one per period", changes)
	}
	step(t, s, 1000)
	if unit(t, s, 1).Alive {
		t.Fatal("the scout outlived a guard that chased it to the wall")
	}
	if guard.order != Move || guard.target != 0 || guard.Moving() {
		t.Fatalf("the attacker kept order %d on %d after its target died", guard.order, guard.target)
	}
}

func TestAttackGivesUpWhenTheTargetBecomesUnreachable(t *testing.T) {
	// Scout 2 shuts the only door and stands in it, so scout 1's attack on guard 3
	// beyond the wall can never come within range. The guard is too far to wake.
	s := build(t, []string{"...#............", "...#............", "................"}, []placed{
		{1, "scout", 0, 0}, {2, "scout", 2, 2}, {3, "guard", 15, 0},
	})
	if !s.Command(2, grid.Cell{X: 3, Y: 2}) || !s.Order(1, Order{Kind: Attack, Target: 3}) {
		t.Fatal("orders refused")
	}
	step(t, s, 400)
	if u := unit(t, s, 1); u.order != Move || u.Moving() {
		t.Fatalf(
			"scout 1 still has order %d, moving %v, with no way to its target",
			u.order,
			u.Moving(),
		)
	}
}

func TestAttackMoveStopsToFightAndResumes(t *testing.T) {
	s := build(t, open(24, 3), []placed{{1, "scout", 0, 0}, {2, "guard", 12, 2}})
	if !s.Order(1, Order{Kind: AttackMove, Goal: grid.Cell{X: 23, Y: 0}}) {
		t.Fatal("order refused")
	}
	scout, guard := unit(t, s, 1), unit(t, s, 2)
	paused := false
	for range 600 {
		before := scout.Center()
		fighting := guard.Alive && inRange(scout, guard)
		step(t, s, 1)
		if fighting && scout.Center() != before {
			t.Fatalf("tick %d: an attack-move walked with an enemy in range", s.Ticks())
		}
		paused = paused || fighting
	}
	if !paused {
		t.Fatal("the attack-move never met the guard")
	}
	if guard.Alive || !scout.Alive || cellOfUnit(s, scout) != (grid.Cell{X: 23, Y: 0}) ||
		scout.order != Move {
		t.Fatalf(
			"after the fight: guard alive %v, scout alive %v at %v with order %d; want the scout at its goal",
			guard.Alive,
			scout.Alive,
			cellOfUnit(s, scout),
			scout.order,
		)
	}
}

func TestHoldFireStopsShootingAndSurvivesOrders(t *testing.T) {
	s := build(t, open(10, 2), []placed{{1, "scout", 0, 0}, {2, "guard", 3, 0}, {3, "guard", 9, 1}})
	if !s.SetHold(1, true) || s.SetHold(7, true) {
		t.Fatal("SetHold accepted an unknown unit or refused a living one")
	}
	if !s.Order(1, Order{Kind: AttackMove, Goal: grid.Cell{X: 0, Y: 1}}) || !unit(t, s, 1).Hold() {
		t.Fatal("an order cleared the stance")
	}
	step(t, s, 60)
	if unit(t, s, 2).HP != guardHP {
		t.Fatal("a held unit fired")
	}
	if u := unit(t, s, 1); cellOfUnit(s, u) != (grid.Cell{X: 0, Y: 1}) || u.Moving() {
		t.Fatal("a held attack-move stopped for an enemy it may not shoot")
	}
	s.SetHold(1, false)
	step(t, s, 15)
	if unit(t, s, 2).HP == guardHP {
		t.Fatal("a unit did not fire after its stance was cleared")
	}
}

func TestGuardOnAnAttackMoveLeavesTheAIAndReturnsToIt(t *testing.T) {
	s := build(t, open(20, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 7, 0}})
	if !s.Order(2, Order{Kind: AttackMove, Goal: grid.Cell{X: 19, Y: 0}}) {
		t.Fatal("order refused")
	}
	// The scout is within sight, but the guard walks away under orders.
	step(t, s, 300)
	g := unit(t, s, 2)
	if g.chasing || cellOfUnit(s, g) != (grid.Cell{X: 19, Y: 0}) {
		t.Fatalf("guard at %v, hunting %v; want it at its goal", cellOfUnit(s, g), g.chasing)
	}
	s.Command(1, grid.Cell{X: 14, Y: 0})
	step(t, s, 300)
	if !g.chasing && unit(t, s, 1).Alive {
		t.Fatal("the guard did not return to its AI after the order ended")
	}
}

func TestAssignGoals(t *testing.T) {
	cell := func(x, y int) grid.Cell { return grid.Cell{X: x, Y: y} }
	tests := []struct {
		name  string
		rows  []string
		units []placed
		ids   []uint64
		click grid.Cell
		want  []Assignment
	}{{
		// Units 1, 2, and 3 are the same distance from the click, so they go in ID order, and unit 4
		// is farther. The click comes first, then the cells one step away in row-major order.
		name:  "ties",
		rows:  open(5, 5),
		units: []placed{{1, "scout", 0, 2}, {3, "scout", 4, 2}, {2, "scout", 2, 0}, {4, "scout", 0, 0}},
		ids:   []uint64{4, 3, 2, 1},
		click: cell(2, 2),
		want:  []Assignment{{1, cell(2, 2)}, {2, cell(2, 1)}, {3, cell(1, 2)}, {4, cell(3, 2)}},
	}, {
		name:  "walls and idle units block the search",
		rows:  []string{".#...", ".#...", "....."},
		units: []placed{{1, "scout", 4, 0}, {2, "scout", 4, 1}, {7, "guard", 0, 2}},
		ids:   []uint64{1, 2},
		click: cell(0, 0),
		want:  []Assignment{{1, cell(0, 0)}, {2, cell(0, 1)}},
	}, {
		name:  "more units than reachable cells",
		rows:  []string{"..#...", "###..."},
		units: []placed{{1, "scout", 5, 0}, {2, "scout", 5, 1}, {3, "scout", 4, 1}},
		ids:   []uint64{1, 2, 3},
		click: cell(1, 0),
		want:  []Assignment{{3, cell(1, 0)}, {1, cell(0, 0)}},
	}, {
		name:  "click on a wall",
		rows:  []string{"..#.."},
		units: []placed{{1, "scout", 0, 0}},
		ids:   []uint64{1},
		click: cell(2, 0),
	}, {
		name:  "click on an idle unit",
		rows:  open(3, 1),
		units: []placed{{1, "scout", 0, 0}, {2, "guard", 2, 0}},
		ids:   []uint64{1},
		click: cell(2, 0),
	}, {
		name:  "click off the board",
		rows:  open(3, 1),
		units: []placed{{1, "scout", 0, 0}},
		ids:   []uint64{1},
		click: cell(3, 0),
	}, {
		name:  "unknown and repeated IDs",
		rows:  open(4, 1),
		units: []placed{{1, "scout", 0, 0}},
		ids:   []uint64{1, 8, 1},
		click: cell(3, 0),
		want:  []Assignment{{1, cell(3, 0)}},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := build(t, tc.rows, tc.units)
			got := s.AssignGoals(nil, tc.ids, tc.click)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("assignments = %v, want %v", got, tc.want)
			}
			// The search leaves no state behind.
			if again := s.AssignGoals(nil, tc.ids, tc.click); !slices.Equal(again, tc.want) {
				t.Fatalf("second call = %v, want %v", again, tc.want)
			}
		})
	}
}

func TestOrderGroupSpreadsTheGroupWithoutAllocating(t *testing.T) {
	s := build(
		t,
		open(12, 6),
		[]placed{{1, "scout", 0, 0}, {2, "scout", 0, 1}, {3, "scout", 0, 2}, {4, "scout", 0, 3}},
	)
	ids := []uint64{1, 2, 3, 4}
	if n := s.OrderGroup(ids, Order{Kind: AttackMove, Goal: grid.Cell{X: 9, Y: 3}}); n != 4 {
		t.Fatalf("%d of 4 units accepted the group order", n)
	}
	step(t, s, 400)
	seen := map[grid.Cell]bool{}
	for _, id := range ids {
		u := unit(t, s, id)
		c := cellOfUnit(s, u)
		if u.Moving() || seen[c] || abs(c.X-9)+abs(c.Y-3) > 1 {
			t.Fatalf(
				"unit %d ended at %v, moving %v; want distinct cells next to the click",
				id,
				c,
				u.Moving(),
			)
		}
		seen[c] = true
	}
	if n := testing.AllocsPerRun(50, func() {
		s.OrderGroup(ids, Order{Kind: Move, Goal: grid.Cell{X: 2, Y: 2}})
		s.OrderGroup(ids, Order{Kind: Move, Goal: grid.Cell{X: 9, Y: 3}})
	}); n != 0 {
		t.Fatalf("OrderGroup allocated %v times", n)
	}
}

func abs(v int) int { return max(v, -v) }

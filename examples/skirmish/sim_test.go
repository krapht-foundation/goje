package skirmish

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/level"
)

// placed describes a unit for a test level.
type placed struct {
	id   int
	kind string
	x, y int
}

// build decodes a level made of terrain rows, units, and optional zone lines.
func build(t *testing.T, rows []string, units []placed, zones ...string) *Sim {
	t.Helper()
	var b strings.Builder
	version := 1
	if len(zones) > 0 {
		version = 2
	}
	fmt.Fprintf(&b, `{"version": %d, "rows": [`, version)
	for i, row := range rows {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", row)
	}
	b.WriteString(`], "placements": [`)
	for i, u := range units {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, `{"id": %d, "kind": %q, "x": %d, "y": %d}`, u.id, u.kind, u.x, u.y)
	}
	b.WriteString("]")
	if len(zones) > 0 {
		b.WriteString(`, "zones": [` + strings.Join(zones, ", ") + "]")
	}
	b.WriteString("}")
	board, err := level.Decode(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("decode level: %v\n%s", err, b.String())
	}
	s, err := New(board)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func step(t *testing.T, s *Sim, n int) {
	t.Helper()
	for range n {
		if err := s.Step(); err != nil {
			t.Fatalf("tick %d: %v", s.Ticks(), err)
		}
	}
}

func unit(t *testing.T, s *Sim, id uint64) *Unit {
	t.Helper()
	u, ok := s.Unit(id)
	if !ok {
		t.Fatalf("no unit %d", id)
	}
	return u
}

func cellOfUnit(s *Sim, u *Unit) grid.Cell {
	c := s.cellOf(u.Center())
	return grid.Cell{X: c % s.width, Y: c / s.width}
}

func atCenter(s *Sim, u *Unit) bool { return !s.offCenter(u) }

// noOverlap fails if two living bodies overlap at any height.
func noOverlap(t *testing.T, s *Sim) {
	t.Helper()
	us := s.Units()
	for i := range us {
		for j := i + 1; j < len(us); j++ {
			if us[i].Alive && us[j].Alive && us[i].Body.Footprint.Overlaps(us[j].Body.Footprint) {
				t.Fatalf("tick %d: units %d and %d overlap", s.Ticks(), us[i].ID, us[j].ID)
			}
		}
	}
}

func open(w, h int) []string {
	rows := make([]string, h)
	for i := range rows {
		rows[i] = strings.Repeat(".", w)
	}
	return rows
}

func TestUnitWalksARouteAndStopsOnTheGoalCenter(t *testing.T) {
	rows := []string{"......", ".###..", "......"}
	s := build(t, rows, []placed{{1, "scout", 0, 0}})
	if !s.Command(1, grid.Cell{X: 5, Y: 2}) {
		t.Fatal("route to a reachable goal was refused")
	}
	step(t, s, 400)
	u := unit(t, s, 1)
	if got := cellOfUnit(s, u); got != (grid.Cell{X: 5, Y: 2}) || !atCenter(s, u) || u.Moving() {
		t.Fatalf(
			"unit ended at %v (center %v, moving %v), want the center of 5, 2",
			got,
			u.Center(),
			u.Moving(),
		)
	}
}

func TestCommandRules(t *testing.T) {
	s := build(
		t,
		[]string{"..#..", ".....", "....."},
		[]placed{{1, "scout", 0, 0}, {2, "scout", 1, 0}},
	)
	tests := []struct {
		name string
		id   uint64
		goal grid.Cell
		want bool
	}{
		{"unknown unit", 9, grid.Cell{X: 4, Y: 2}, false},
		{"outside the board", 1, grid.Cell{X: 5, Y: 0}, false},
		{"negative", 1, grid.Cell{X: -1, Y: 0}, false},
		{"inside a wall", 1, grid.Cell{X: 2, Y: 0}, false},
		{"held by an idle unit", 1, grid.Cell{X: 1, Y: 0}, false},
		{"own cell while stationary", 1, grid.Cell{X: 0, Y: 0}, false},
		{"free cell", 1, grid.Cell{X: 4, Y: 2}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.Command(tc.id, tc.goal); got != tc.want {
				t.Fatalf("Command = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRefusedCommandKeepsTheCurrentOrder(t *testing.T) {
	s := build(t, open(8, 1), []placed{{1, "scout", 0, 0}, {2, "scout", 7, 0}})
	if !s.Command(1, grid.Cell{X: 5, Y: 0}) {
		t.Fatal("first order refused")
	}
	step(t, s, 20)
	if s.Command(1, grid.Cell{X: 7, Y: 0}) {
		t.Fatal("a goal held by an idle unit was accepted")
	}
	step(t, s, 300)
	if got := cellOfUnit(s, unit(t, s, 1)); got != (grid.Cell{X: 5, Y: 0}) {
		t.Fatalf("unit ended at %v, want the first order's goal", got)
	}
}

func TestRoutesGoAroundIdleUnits(t *testing.T) {
	s := build(t, open(5, 3), []placed{{1, "scout", 0, 1}, {2, "scout", 2, 1}})
	if !s.Command(1, grid.Cell{X: 4, Y: 1}) {
		t.Fatal("route around an idle unit was refused")
	}
	step(t, s, 300)
	if got := cellOfUnit(s, unit(t, s, 1)); got != (grid.Cell{X: 4, Y: 1}) {
		t.Fatalf("unit ended at %v", got)
	}
	noOverlap(t, s)
}

func TestUnitsMeetingInOpenGroundGoAroundEachOther(t *testing.T) {
	s := build(t, open(9, 3), []placed{{1, "scout", 0, 1}, {2, "scout", 8, 1}})
	s.Command(1, grid.Cell{X: 8, Y: 0})
	s.Command(2, grid.Cell{X: 0, Y: 2})
	for range 1000 {
		step(t, s, 1)
		noOverlap(t, s)
	}
	for id, want := range map[uint64]grid.Cell{1: {X: 8, Y: 0}, 2: {X: 0, Y: 2}} {
		if got := cellOfUnit(s, unit(t, s, id)); got != want {
			t.Errorf("unit %d ended at %v, want %v", id, got, want)
		}
	}
}

func TestUnitsMeetingHeadOnInACorridorGiveUp(t *testing.T) {
	s := build(t, open(9, 1), []placed{{1, "scout", 0, 0}, {2, "scout", 8, 0}})
	s.Command(1, grid.Cell{X: 8, Y: 0}) // Refused while 2 is idle, so send them toward free cells.
	s.Command(1, grid.Cell{X: 7, Y: 0})
	s.Command(2, grid.Cell{X: 1, Y: 0})
	for range 600 {
		step(t, s, 1)
		noOverlap(t, s)
	}
	for _, id := range []uint64{1, 2} {
		if u := unit(t, s, id); u.Moving() {
			t.Fatalf("unit %d is still trying after 600 ticks", id)
		}
	}
}

func TestEveryOrderEndsEvenWhenTheGoalIsTaken(t *testing.T) {
	s := build(t, open(6, 3), []placed{{1, "scout", 0, 0}, {2, "scout", 0, 1}, {3, "scout", 0, 2}})
	for _, id := range []uint64{1, 2, 3} {
		s.Command(id, grid.Cell{X: 5, Y: 1})
	}
	step(t, s, 1000)
	for _, id := range []uint64{1, 2, 3} {
		if unit(t, s, id).Moving() {
			t.Fatalf("unit %d never finished", id)
		}
	}
	noOverlap(t, s)
}

func TestSolidZonesBlockRoutesOnlyWhereTheyReachTheUnit(t *testing.T) {
	const ground = `{"id": 1, "kind": "crate", "role": "solid", "x": 1, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 8}`
	const deck = `{"id": 2, "kind": "deck", "role": "solid", "x": 3, "y": 0, "w": 1, "h": 1, "bottom": 24, "top": 32}`
	s := build(t, open(5, 1), []placed{{1, "scout", 0, 0}}, ground, deck)
	if s.Command(1, grid.Cell{X: 4, Y: 0}) {
		t.Fatal("a route through a one-cell corridor with a ground-level solid was accepted")
	}
	s = build(t, open(5, 1), []placed{{1, "scout", 2, 0}}, ground, deck)
	if !s.Command(1, grid.Cell{X: 4, Y: 0}) {
		t.Fatal("a raised deck blocked a route under it")
	}
	step(t, s, 200)
	if got := cellOfUnit(s, unit(t, s, 1)); got != (grid.Cell{X: 4, Y: 0}) {
		t.Fatalf("unit ended at %v, want 4, 0", got)
	}
}

func TestNewRejectsBadPlacements(t *testing.T) {
	for name, src := range map[string]string{
		"unknown kind": `{"version":1,"rows":["..."],"placements":[{"id":1,"kind":"lookout","x":0,"y":0}]}`,
		"in a solid zone": `{"version":2,"rows":["..."],"placements":[{"id":1,"kind":"scout","x":0,"y":0}],` +
			`"zones":[{"id":1,"kind":"crate","role":"solid","x":0,"y":0,"w":1,"h":1,"bottom":0,"top":8}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			board, err := level.Decode(strings.NewReader(src))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := New(board); err == nil {
				t.Fatal("New accepted the level")
			}
		})
	}
}

func TestUnitsAreOrderedByID(t *testing.T) {
	s := build(t, open(4, 1), []placed{{9, "scout", 0, 0}, {3, "guard", 3, 0}, {5, "scout", 1, 0}})
	ids := make([]uint64, 0, len(s.Units()))
	for _, u := range s.Units() {
		ids = append(ids, u.ID)
	}
	if fmt.Sprint(ids) != "[3 5 9]" {
		t.Fatalf("order = %v", ids)
	}
}

// randomHistory runs a seeded random crowd and checks the invariants every tick.
func randomHistory(t *testing.T, seed uint64, ticks int) *Sim {
	t.Helper()
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
	units := make([]placed, 0, 32)
	free := [][2]int{}
	for y, row := range rows {
		for x, c := range row {
			if c == '.' && (x+y)%3 == 0 {
				free = append(free, [2]int{x, y})
			}
		}
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	rng.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
	for i := range 14 {
		kind := "scout"
		if i%2 == 1 {
			kind = "guard"
		}
		units = append(units, placed{i + 1, kind, free[i][0], free[i][1]})
	}
	s := build(t, rows, units)
	for tick := range ticks {
		if tick%7 == 0 {
			id := uint64(rng.IntN(14) + 1)
			s.Command(id, grid.Cell{X: rng.IntN(12), Y: rng.IntN(8)})
		}
		step(t, s, 1)
		noOverlap(t, s)
		for _, u := range s.Units() {
			if !u.Alive {
				continue
			}
			f := u.Body.Footprint
			if f.Min.X < 0 || f.Min.Y < 0 || f.Max.X > fixed.FromInt(12) || f.Max.Y > fixed.FromInt(8) {
				t.Fatalf("tick %d: unit %d left the board", s.Ticks(), u.ID)
			}
		}
	}
	return s
}

func TestRandomHistoriesNeverOverlapAndReplayIdentically(t *testing.T) {
	for seed := uint64(1); seed <= 12; seed++ {
		first := randomHistory(t, seed, 1500)
		second := randomHistory(t, seed, 1500)
		if first.Trace() != second.Trace() {
			t.Fatalf("seed %d: two runs of one history gave different traces", seed)
		}
	}
	if randomHistory(t, 1, 300).Trace() == randomHistory(t, 2, 300).Trace() {
		t.Fatal("different histories gave the same trace")
	}
}

func TestStepDoesNotAllocate(t *testing.T) {
	s := randomHistory(t, 3, 200)
	allocs := testing.AllocsPerRun(200, func() {
		if err := s.Step(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Step allocated %v times per tick", allocs)
	}
}

func TestBoltsStartOutsideTheirShooter(t *testing.T) {
	// Every aim direction must clear the shooter's own footprint.
	for dx := -8; dx <= 8; dx++ {
		for dy := -8; dy <= 8; dy++ {
			if dx == 0 && dy == 0 {
				continue
			}
			aim := fixed.Vec2{X: fixed.Num(dx * 64), Y: fixed.Num(dy * 64)}.WithLength(muzzle)
			bolt := bodyAt(aim)
			bolt.Min.X, bolt.Min.Y = aim.X.Sub(boltSize/2), aim.Y.Sub(boltSize/2)
			bolt.Max.X, bolt.Max.Y = aim.X.Add(boltSize/2), aim.Y.Add(boltSize/2)
			if bolt.Overlaps(bodyAt(fixed.Vec2{})) {
				t.Fatalf("aim %d, %d starts inside the shooter", dx, dy)
			}
		}
	}
}

func TestWeaponKillsAfterThreeShotsAndRemovesTheBody(t *testing.T) {
	s := build(t, open(8, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 3, 0}})
	step(t, s, 1)
	if unit(t, s, 2).HP != guardHP {
		t.Fatal("damage before any bolt could arrive")
	}
	step(t, s, 200)
	guard, scout := unit(t, s, 2), unit(t, s, 1)
	// Both fire every 30 ticks from tick 1, so their third bolts land together: the guard dies and
	// the scout is left with 10.
	if want := scoutHP - 3*weaponDamage; guard.Alive || guard.HP != 0 || scout.HP != want {
		t.Fatalf(
			"guard alive %v HP %d, scout HP %d; want a dead guard and a scout with %d",
			guard.Alive,
			guard.HP,
			scout.HP,
			want,
		)
	}
	if _, ok := s.bodies.Get(2); ok {
		t.Fatal("the dead guard is still a body")
	}
	if len(s.Bolts()) != 0 {
		t.Fatalf("%d bolts still flying after the fight", len(s.Bolts()))
	}
}

func TestWallsStopBoltsAndDecksDoNot(t *testing.T) {
	// A wall between two units absorbs every shot.
	s := build(t, []string{"..#.."}, []placed{{1, "scout", 1, 0}, {2, "guard", 3, 0}})
	step(t, s, 20)
	if unit(t, s, 1).HP != scoutHP || unit(t, s, 2).HP != guardHP {
		t.Fatal("a bolt crossed a wall")
	}
	// A raised deck leaves the bolt's height clear.
	const deck = `{"id": 1, "kind": "deck", "role": "solid", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 24, "top": 32}`
	s = build(t, open(5, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 0}}, deck)
	step(t, s, 40)
	if unit(t, s, 2).HP == guardHP || unit(t, s, 1).HP == scoutHP {
		t.Fatalf(
			"bolts did not cross under the deck: scout %d, guard %d",
			unit(t, s, 1).HP,
			unit(t, s, 2).HP,
		)
	}
}

func TestBoltsHurtFriends(t *testing.T) {
	s := build(t, open(6, 1), []placed{{1, "scout", 0, 0}, {2, "scout", 1, 0}, {3, "guard", 4, 0}})
	step(t, s, 40)
	// Scout 1 is the only one that cannot reach the guard without passing scout 2.
	if unit(t, s, 2).HP == scoutHP && unit(t, s, 1).HP == scoutHP {
		t.Fatal("no scout was hurt by a friend's bolt or the guard's")
	}
	if unit(t, s, 3).HP == guardHP {
		t.Fatal("the guard was never hit")
	}
}

func TestGuardsWakeOnSightAndStayAsleepBeyondIt(t *testing.T) {
	s := build(t, open(20, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 10, 0}})
	step(t, s, 10)
	if g := unit(t, s, 2); g.chasing || g.Moving() {
		t.Fatal("a guard woke without a scout in sight")
	}
	s.Command(1, grid.Cell{X: 4, Y: 0})
	step(t, s, 200)
	if g := unit(t, s, 2); !g.chasing {
		t.Fatal("a guard missed a scout within sight")
	}
}

func TestAlarmZoneWakesEveryGuardAtAnyDistance(t *testing.T) {
	const alarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 32}`
	s := build(t, open(30, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 29, 0}}, alarm)
	step(t, s, 5)
	if s.Alarm() || unit(t, s, 2).chasing {
		t.Fatal("alarm rang before a scout entered the zone")
	}
	s.Command(1, grid.Cell{X: 3, Y: 0})
	step(t, s, 100)
	if !s.Alarm() {
		t.Fatal("alarm did not ring")
	}
	g := unit(t, s, 2)
	if !g.chasing || g.Center().X >= fixed.FromInt(29) {
		t.Fatalf("the guard did not start hunting: chasing %v, center %v", g.chasing, g.Center())
	}
}

func TestGuardsOnlyReactToScouts(t *testing.T) {
	const alarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 2, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 32}`
	s := build(t, open(20, 1), []placed{{1, "guard", 0, 0}, {2, "guard", 19, 0}}, alarm)
	s.Command(1, grid.Cell{X: 3, Y: 0})
	step(t, s, 100)
	if s.Alarm() {
		t.Fatal("a guard rang the alarm")
	}
}

func TestBattleOfSquadsEndsWithDeadUnitsOutOfTheWorld(t *testing.T) {
	rows := open(24, 12)
	units := make([]placed, 0, 32)
	for i := range 6 {
		units = append(units, placed{i + 1, "scout", 1, 1 + i*2}, placed{i + 101, "guard", 22, 1 + i*2})
	}
	s := build(t, rows, units)
	for i := range 6 {
		s.Command(uint64(i+1), grid.Cell{X: 16, Y: 1 + i*2})
	}
	for range 900 {
		step(t, s, 1)
		noOverlap(t, s)
	}
	dead := 0
	for _, u := range s.Units() {
		if !u.Alive {
			dead++
			if _, ok := s.bodies.Get(u.ID); ok {
				t.Fatalf("dead unit %d is still a body", u.ID)
			}
		}
	}
	if dead == 0 {
		t.Fatal("nobody died in a 6 against 6 fight")
	}
}

func TestRandomSquadsFightReplayably(t *testing.T) {
	run := func() uint64 {
		s := build(t, open(20, 10), squadUnits(16, 20, 10))
		for tick := range 1200 {
			if tick == 0 {
				for _, u := range s.Units() {
					if u.Team == Scouts {
						s.Command(u.ID, grid.Cell{X: 10, Y: int(u.ID) % 10})
					}
				}
			}
			step(t, s, 1)
		}
		return s.Trace()
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("two runs differ: %d and %d", a, b)
	}
}

// squadUnits places n scouts on the west edge and n guards on the east edge.
func squadUnits(n, w, h int) []placed {
	units := make([]placed, 0, 32)
	for i := range n {
		x, y := i/h, i%h
		units = append(units, placed{i + 1, "scout", x, y}, placed{i + 1001, "guard", w - 1 - x, y})
	}
	return units
}

func TestTargetTiesGoToTheLowestID(t *testing.T) {
	// The scout is the same distance from both guards, so its first bolt goes to guard 3.
	s := build(t, open(7, 1), []placed{{1, "scout", 3, 0}, {5, "guard", 0, 0}, {3, "guard", 6, 0}})
	step(t, s, 15)
	if low, high := unit(
		t,
		s,
		3,
	).HP, unit(
		t,
		s,
		5,
	).HP; low != guardHP-weaponDamage ||
		high != guardHP {
		t.Fatalf(
			"guard 3 has %d health and guard 5 has %d; want %d and %d",
			low,
			high,
			guardHP-weaponDamage,
			guardHP,
		)
	}
}

func TestGuardsStandDownWhenNoScoutIsLeft(t *testing.T) {
	s := build(t, open(10, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 5, 0}, {3, "guard", 6, 0}})
	step(t, s, 300)
	if unit(t, s, 1).Alive {
		t.Fatal("two guards did not defeat one scout")
	}
	// The scout fires at the nearest guard, so guard 2 falls and guard 3 survives to stand down.
	if g := unit(t, s, 3); !g.Alive || g.chasing || g.Moving() {
		t.Fatalf(
			"guard 3: alive %v, hunting %v, moving %v; want a living guard at rest",
			g.Alive,
			g.chasing,
			g.Moving(),
		)
	}
}

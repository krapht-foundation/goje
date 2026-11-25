package skirmish

import (
	"bytes"
	"math/bits"
	"reflect"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

func TestSetControlRules(t *testing.T) {
	s := build(t, open(5, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 0}})
	if s.SetControl(0, true) == nil || s.SetControl(Guards+1, true) == nil {
		t.Fatal("an unknown team was accepted")
	}
	if err := s.SetControl(Guards, true); err != nil {
		t.Fatal(err)
	}
	if g := unit(t, s, 2); g.HP != scoutHP || g.MaxHP != scoutHP {
		t.Fatalf("human guards have health %d of %d, want the scouts' %d", g.HP, g.MaxHP, scoutHP)
	}
	if err := s.SetControl(Guards, false); err != nil {
		t.Fatal(err)
	}
	if g := unit(t, s, 2); g.HP != guardHP || g.MaxHP != guardHP {
		t.Fatal("AI guards did not get their own health back")
	}
	step(t, s, 1)
	if s.SetControl(Guards, true) == nil || s.SetTactics(true) == nil {
		t.Fatal("an option was set after a Step")
	}
}

// TestHumanGuardsKeepTheirOrders sends a guard away from a scout it would
// hunt. The AI turns it back; a human-controlled guard keeps walking, and the
// alarm does not wake it.
func TestHumanGuardsKeepTheirOrders(t *testing.T) {
	const alarm = `{"id": 7, "kind": "alarm", "role": "trigger", "x": 0, "y": 0, "w": 1, "h": 1, "bottom": 0, "top": 32}`
	for _, human := range []bool{false, true} {
		s := build(t, open(20, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 5, 0}}, alarm)
		if err := s.SetControl(Guards, human); err != nil {
			t.Fatal(err)
		}
		if !s.Command(2, grid.Cell{X: 19, Y: 0}) {
			t.Fatal("the guard's order was refused")
		}
		step(t, s, 20)
		g := unit(t, s, 2)
		goal, _ := s.Goal(g)
		switch {
		case human && (g.chasing || g.alarmed || goal != grid.Cell{X: 19, Y: 0}):
			t.Fatalf(
				"a human guard ran the AI: chasing %v, alarmed %v, goal %v",
				g.chasing,
				g.alarmed,
				goal,
			)
		case !human && !g.chasing:
			t.Fatal("an AI guard did not hunt the scout")
		}
	}
}

func TestWinnerIsTheLastTeamStanding(t *testing.T) {
	s := build(t, open(5, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 0}, {3, "scout", 2, 0}})
	if _, ok := s.Winner(); ok {
		t.Fatal("a match with both teams alive is decided")
	}
	s.units[1].Alive = false
	if team, ok := s.Winner(); !ok || team != Scouts {
		t.Fatalf("Winner = %d, %v; want the scouts", team, ok)
	}
	s.units[1].Alive, s.units[0].Alive, s.units[2].Alive = true, false, false
	if team, ok := s.Winner(); !ok || team != Guards {
		t.Fatalf("Winner = %d, %v; want the guards", team, ok)
	}
	s.units[1].Alive = false
	if team, ok := s.Winner(); !ok || team != 0 {
		t.Fatalf("Winner = %d, %v; want a draw", team, ok)
	}
}

// TestVersusMatchEndsWithAWinner plays both teams with the same orders: each
// attack-moves to the other's side. Someone wins.
func TestVersusMatchEndsWithAWinner(t *testing.T) {
	s := build(t, open(12, 6), squadUnits(6, 12, 6))
	if err := s.SetControl(Guards, true); err != nil {
		t.Fatal(err)
	}
	withVision(t, s, 5)
	var scouts, guards []uint64
	for _, u := range s.Units() {
		if u.Team == Scouts {
			scouts = append(scouts, u.ID)
		} else {
			guards = append(guards, u.ID)
		}
	}
	for tick := range 3000 {
		if tick%200 == 0 {
			s.OrderGroup(scouts, Order{Kind: AttackMove, Goal: grid.Cell{X: 10, Y: 3}})
			s.OrderGroup(guards, Order{Kind: AttackMove, Goal: grid.Cell{X: 1, Y: 3}})
		}
		step(t, s, 1)
		noOverlap(t, s)
		if _, ok := s.Winner(); ok {
			return
		}
	}
	t.Fatal("the versus match never ended")
}

func TestGuardsExploreOnlyWhenAsked(t *testing.T) {
	rows, units := open(9, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 8, 0}}
	plain := withVision(t, build(t, rows, units), 2)
	versus := build(t, rows, units)
	if err := versus.EnableVision(VisionConfig{Radius: 2, GuardsExplore: true}); err != nil {
		t.Fatal(err)
	}
	near, far := grid.Cell{X: 6, Y: 0}, grid.Cell{X: 1, Y: 0}
	if plain.ExploredBy(Guards, near) || !plain.ExploredBy(Scouts, far) {
		t.Fatal("without GuardsExplore the guards remember cells, or the scouts forgot")
	}
	if !versus.ExploredBy(Guards, near) || versus.ExploredBy(Guards, far) ||
		versus.ExploredBy(0, near) {
		t.Fatal("the guards' explored set is wrong at the start")
	}
	versus.Command(2, grid.Cell{X: 3, Y: 0})
	step(t, versus, 200)
	versus.Command(2, grid.Cell{X: 8, Y: 0})
	step(t, versus, 200)
	if !versus.ExploredBy(Guards, far) || versus.Visible(Guards, far) {
		t.Fatal("the guards did not remember a cell they saw and left")
	}
	for c := range 9 {
		cell := grid.Cell{X: c}
		if versus.Explored(cell) != versus.ExploredBy(Scouts, cell) {
			t.Fatal("Explored is not ExploredBy for the scouts")
		}
	}
}

func TestBlockedCellsHideLikeWalls(t *testing.T) {
	rows, units := open(5, 1), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 0}}
	s := build(t, rows, units)
	if s.EnableVision(VisionConfig{Radius: 7, Blocked: make([]bool, 4)}) == nil {
		t.Fatal("a mask of the wrong size was accepted")
	}
	mask := make([]bool, 5)
	mask[2] = true
	if err := s.EnableVision(VisionConfig{Radius: 7, Blocked: mask}); err != nil {
		t.Fatal(err)
	}
	if !s.Visible(Scouts, grid.Cell{X: 2}) || s.Visible(Scouts, grid.Cell{X: 3}) ||
		s.Visible(Guards, grid.Cell{X: 1}) {
		t.Fatal("a blocked cell does not cast a wall's shadow from both sides")
	}
	if !s.Command(1, grid.Cell{X: 2}) {
		t.Fatal("a blocked cell stopped a route")
	}
}

func TestSnapshotVersionFollowsTheOptions(t *testing.T) {
	rows, units := open(5, 3), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 2}}
	cases := []struct {
		name    string
		setup   func(*Sim) error
		version uint16
	}{
		{"default", func(*Sim) error { return nil }, 1},
		{"vision", func(s *Sim) error { return s.EnableVision(VisionConfig{Radius: 3}) }, 2},
		{"blocked mask", func(s *Sim) error {
			return s.EnableVision(VisionConfig{Radius: 3, Blocked: make([]bool, 15)})
		}, 2},
		{"default control", func(s *Sim) error { return s.SetControl(Scouts, true) }, 1},
		{"tactics off", func(s *Sim) error { return s.SetTactics(false) }, 1},
		{"human guards", func(s *Sim) error { return s.SetControl(Guards, true) }, 3},
		{"AI scouts", func(s *Sim) error { return s.SetControl(Scouts, false) }, 3},
		{"tactics", func(s *Sim) error { return s.SetTactics(true) }, 3},
		{"guards explore", func(s *Sim) error {
			return s.EnableVision(VisionConfig{Radius: 3, GuardsExplore: true})
		}, 3},
	}
	snaps := make([][]byte, len(cases))
	for i, c := range cases {
		s := build(t, rows, units)
		if err := c.setup(s); err != nil {
			t.Fatal(err)
		}
		snaps[i], _ = s.Snapshot()
		if v := uint16(snaps[i][0]) | uint16(snaps[i][1])<<8; v != c.version {
			t.Errorf("%s: snapshot version %d, want %d", c.name, v, c.version)
		}
	}
	if !bytes.Equal(snaps[0], snaps[3]) || !bytes.Equal(snaps[0], snaps[4]) {
		t.Error("setting an option to its default changed the snapshot")
	}
	// A Sim refuses a snapshot taken with other options.
	for i, c := range cases {
		s := build(t, rows, units)
		if err := c.setup(s); err != nil {
			t.Fatal(err)
		}
		for j, snap := range snaps {
			same := reflect.DeepEqual(snap, snaps[i])
			if err := s.Restore(snap); (err == nil) != same {
				t.Errorf("%s restoring %s: %v", c.name, cases[j].name, err)
			}
		}
	}
}

func TestVersusSnapshotRejectsDamage(t *testing.T) {
	rows, units := open(5, 3), []placed{{1, "scout", 0, 0}, {2, "guard", 4, 2}}
	s := build(t, rows, units)
	if err := s.SetControl(Guards, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTactics(true); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVision(VisionConfig{Radius: 3, GuardsExplore: true}); err != nil {
		t.Fatal(err)
	}
	snap, _ := s.Snapshot()
	// After the version 1 layout: options, radius, two explored sets of two
	// bytes, a last-seen cell and a waypoint for each unit.
	at := len(snap) - (1 + 1 + 2*2 + 4*2 + 4*2)
	if snap[at] != optionGuardsHuman|optionTactics|optionGuardsExplore|optionVision {
		t.Fatalf("options byte %#x", snap[at])
	}
	set := func(i int, v ...byte) []byte {
		b := slices.Clone(snap)
		copy(b[i:], v)
		return b
	}
	end := len(snap) - 8
	cases := map[string][]byte{
		"other options":                 set(at, snap[at]&^optionTactics),
		"guards explored off the board": set(at+5, 0x80),
		"waypoint below flankDone":      set(end, 0xfd, 0xff, 0xff, 0xff),
		"waypoint off the board":        set(end, 15, 0, 0, 0),
		"trailing data":                 append(slices.Clone(snap), 0),
		"the version 2 form of the Sim": slices.Concat([]byte{2, 0}, snap[2:at], snap[at+1:]),
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
	if after, _ := s.Snapshot(); !bytes.Equal(after, snap) {
		t.Fatal("a rejected Restore changed the Sim")
	}
	if err := s.Restore(set(end, 0xfe, 0xff, 0xff, 0xff)); err != nil {
		t.Fatalf("a done flank was refused: %v", err)
	}
}

// TestRandomVersusHistories runs seeded order histories with human guards
// that explore, and with tactics: no footprints overlap, each team's explored
// set only grows and holds what it sees, the runs replay identically, and
// restoring at several points continues identically.
func TestRandomVersusHistories(t *testing.T) {
	const total = 900
	for seed := uint64(1); seed <= 4; seed++ {
		for _, h := range []orderHistory{
			{radius: 4, versus: true},
			{radius: 4, tactics: true},
			{tactics: true},
		} {
			base := newOrderHistory(seed)
			h.seed, h.units = base.seed, base.units
			first, second := h.build(t), h.build(t)
			var was [2]int
			for tick := range total {
				h.run(t, first, tick, tick+1)
				for i, team := range []Team{Scouts, Guards} {
					set, n := first.exploredBy(team), 0
					for _, w := range set {
						n += bits.OnesCount64(w)
					}
					if n < was[i] {
						t.Fatalf("seed %d tick %d: team %d's explored cells shrank", seed, tick, team)
					}
					was[i] = n
					for c := range first.width * first.height {
						cell := grid.Cell{X: c % first.width, Y: c / first.width}
						if set != nil && first.Visible(team, cell) && !first.ExploredBy(team, cell) {
							t.Fatalf("seed %d tick %d: team %d sees an unexplored cell", seed, tick, team)
						}
					}
				}
			}
			h.run(t, second, 0, total)
			if first.Trace() != second.Trace() {
				t.Fatalf("seed %d %+v: two runs gave different traces", seed, h)
			}
			for _, at := range []int{1, 240, 700} {
				original := h.build(t)
				h.run(t, original, 0, at)
				snap, _ := original.Snapshot()
				restored := h.build(t)
				if err := restored.Restore(snap); err != nil {
					t.Fatalf("seed %d tick %d: %v", seed, at, err)
				}
				h.run(t, original, at, total)
				h.run(t, restored, at, total)
				if a, b := stateOf(original), stateOf(restored); !reflect.DeepEqual(a, b) {
					t.Fatalf("seed %d tick %d %+v: restored run ended in another state", seed, at, h)
				}
			}
		}
	}
}

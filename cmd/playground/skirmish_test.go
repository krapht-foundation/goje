package main

import (
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/grid"
)

func newSkirmishScene(t *testing.T) *skirmishScene {
	t.Helper()
	board, err := loadSkirmish(authoredFiles)
	if err != nil {
		t.Fatal(err)
	}
	s := &skirmishScene{board: board}
	if err := s.Enter(); err != nil {
		t.Fatal(err)
	}
	return s
}

// logical returns the logical viewport pixel of a ground point at the opening camera.
func (s *skirmishScene) logical(x, y, elevation float64) (screenX, screenY float64) {
	return s.camera.ToLogical(courtyardProjection.Project(x, y, elevation))
}

func (s *skirmishScene) update(t *testing.T, input engine.Input) {
	t.Helper()
	if _, err := s.Update(engine.Tick{Input: input}); err != nil {
		t.Fatal(err)
	}
}

func TestSkirmishSelectsOnlyScoutsAndOrdersTheSelection(t *testing.T) {
	s := newSkirmishScene(t)
	press := func(x, y, elevation float64) engine.Input {
		px, py := s.logical(x, y, elevation)
		return engine.Input{PrimaryPress: engine.MousePress{X: px, Y: py, Valid: true}}
	}

	s.update(t, press(8.5, 2.5, 7)) // A guard.
	if s.selected != 0 {
		t.Fatalf("a press on a guard selected unit %d", s.selected)
	}
	gx, gy := s.logical(6.5, 2.5, 0)
	s.update(t, engine.Input{SecondaryPress: engine.MousePress{X: gx, Y: gy, Valid: true}})
	for _, u := range s.sim.Units() {
		if u.Moving() {
			t.Fatalf("unit %d got an order with nothing selected", u.ID)
		}
	}

	s.update(t, press(1.5, 2.5, 7)) // Scout 1.
	if s.selected != 1 {
		t.Fatalf("a press on scout 1 selected unit %d", s.selected)
	}
	s.update(t, engine.Input{SecondaryPress: engine.MousePress{X: gx, Y: gy, Valid: true}})
	u, _ := s.sim.Unit(1)
	if goal, ok := s.sim.Goal(u); !ok || goal != (grid.Cell{X: 6, Y: 2}) {
		t.Fatalf("scout 1 goal = %v, %v; want 6, 2", goal, ok)
	}

	s.update(t, press(1.5, 5.5, 200)) // Empty ground clears the selection.
	if s.selected != 0 {
		t.Fatalf("a press on empty ground left unit %d selected", s.selected)
	}
}

// The recorded fight: scout 1 is sent across the alarm line, the guards wake and
// shoot, scout 1 and both guards fall, and scout 2 survives with 20 health. The
// trace pins the whole history, so it must be identical natively and as WebAssembly.
func TestSkirmishFightHasTheRecordedOutcome(t *testing.T) {
	const wantTrace uint64 = 114674908196039773
	s := newSkirmishScene(t)
	px, py := s.logical(1.5, 2.5, 7)
	s.update(t, engine.Input{PrimaryPress: engine.MousePress{X: px, Y: py, Valid: true}})
	gx, gy := s.logical(6.5, 2.5, 0)
	s.update(t, engine.Input{SecondaryPress: engine.MousePress{X: gx, Y: gy, Valid: true}})
	for range 400 {
		s.update(t, engine.Input{})
	}
	want := map[uint64]struct {
		alive bool
		hp    int
	}{1: {false, 0}, 2: {true, 20}, 11: {false, 0}, 12: {false, 0}}
	for _, u := range s.sim.Units() {
		if w := want[u.ID]; u.Alive != w.alive || (u.Alive && u.HP != w.hp) {
			t.Errorf(
				"unit %d: alive %v health %d; want alive %v health %d",
				u.ID,
				u.Alive,
				u.HP,
				w.alive,
				w.hp,
			)
		}
	}
	if !s.sim.Alarm() {
		t.Error("the alarm never rang")
	}
	if s.selected != 0 {
		t.Errorf("fallen scout is still selected: %d", s.selected)
	}
	if got := s.sim.Trace(); got != wantTrace {
		t.Errorf("trace = %d, want %d", got, wantTrace)
	}
}

func TestSkirmishRestartRestoresTheOpening(t *testing.T) {
	t.Parallel()

	project, err := loadAuthored(authoredFiles)
	if err != nil {
		t.Fatal(err)
	}
	board, err := loadSkirmish(authoredFiles)
	if err != nil {
		t.Fatal(err)
	}
	factory := project.skirmishFactory(board)
	var scene *skirmishScene
	runtime, err := engine.Start(func() engine.Scene {
		scene = factory().(*skirmishScene)
		return scene
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	px, py := scene.logical(1.5, 2.5, 7)
	if err := runtime.Step(
		engine.Input{PrimaryPress: engine.MousePress{X: px, Y: py, Valid: true}},
	); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if err := runtime.Step(engine.Input{}); err != nil {
			t.Fatal(err)
		}
	}
	previous := scene
	if err := runtime.Step(engine.Input{Pressed: engine.KeyR}); err != nil {
		t.Fatal(err)
	}
	if scene == previous || scene.sim.Ticks() != 0 || scene.selected != 0 {
		t.Fatalf("restart kept the old scene: new scene %v, ticks %d, selected %d",
			scene != previous, scene.sim.Ticks(), scene.selected)
	}
	for _, u := range scene.sim.Units() {
		if !u.Alive || u.HP != u.MaxHP {
			t.Errorf("unit %d after restart: alive %v health %d of %d", u.ID, u.Alive, u.HP, u.MaxHP)
		}
	}
}

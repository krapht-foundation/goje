package main

import (
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/grid"
	courtyardlevel "github.com/krapht-foundation/goje/examples/courtyard"
)

func TestAuthoredCommandsUseTickStartCameraAndRestoreOnRestart(t *testing.T) {
	factory, err := authoredFactory()
	if err != nil {
		t.Fatal(err)
	}
	var scene *authoredCourtyard
	runtime, err := engine.Start(func() engine.Scene {
		scene = factory().(*authoredCourtyard)
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
	step := func(input engine.Input) {
		t.Helper()
		if err := runtime.Step(input); err != nil {
			t.Fatal(err)
		}
	}
	spawn, camera := scene.cell, scene.camera
	goal := grid.Cell{X: 6, Y: 2}
	ground := authoredPress(scene, float64(goal.X)+0.5, float64(goal.Y)+0.5, 0)
	step(engine.Input{SecondaryPress: ground})
	if scene.substep != 0 || len(scene.route) != 0 {
		t.Fatal("secondary press issued an order without selection")
	}
	step(engine.Input{
		PrimaryPress:   authoredPress(scene, float64(spawn.X)+0.5, float64(spawn.Y)+0.5, 12),
		SecondaryPress: ground,
		Pressed:        engine.KeyW,
		Held:           engine.KeyArrowRight,
	})
	if !scene.selected || scene.substep != 1 || len(scene.route) == 0 ||
		scene.route[len(scene.route)-1] != goal || scene.camera == camera {
		t.Fatalf("same-tick selection, command, and camera update did not resolve: %+v", scene)
	}
	previous := scene
	step(engine.Input{Pressed: engine.KeyR, PrimaryPress: ground, SecondaryPress: ground})
	if previous.substep != 1 || !previous.selected {
		t.Fatal("restart tick processed input or movement on the previous scene")
	}
	if scene == previous || scene.cell != spawn || scene.camera != camera || scene.selected ||
		len(scene.route) != 0 {
		t.Fatalf("restart did not restore authored state: %+v", scene)
	}
}

func TestAuthoredRedirectsKeepTheCurrentEdge(t *testing.T) {
	factory, err := authoredFactory()
	if err != nil {
		t.Fatal(err)
	}
	s := factory().(*authoredCourtyard)
	if err := s.Enter(); err != nil {
		t.Fatal(err)
	}
	step := func(input engine.Input) {
		t.Helper()
		if _, err := s.Update(engine.Tick{Input: input}); err != nil {
			t.Fatal(err)
		}
	}
	order := func(x, y float64) { step(engine.Input{SecondaryPress: authoredPress(s, x, y, 0)}) }
	step(engine.Input{PrimaryPress: authoredPress(s, 2.5, 2.5, 12)})
	order(2.5, 2.5) // A stationary scout is already at this destination.
	if len(s.route) != 0 {
		t.Fatal("current center should be a no-op")
	}
	order(6.5, 2.5)
	for _, target := range [][2]float64{{4.5, 2.5}, {7.5, 4.5}, {-0.01, 2.5}} {
		order(target[0], target[1])
		if !s.selected || len(s.route) == 0 || s.route[len(s.route)-1] != (grid.Cell{X: 6, Y: 2}) {
			t.Fatalf("invalid order %v replaced the active route or cleared selection", target)
		}
	}
	// The new destination is behind the scout. It must finish its current
	// eastward edge first, then reverse from the exact center.
	order(2.5, 2.5)
	for range scoutEdgeTicks - s.substep {
		step(engine.Input{})
	}
	if s.cell != (grid.Cell{X: 3, Y: 2}) || s.next == len(s.route) {
		t.Fatalf("redirect skipped the committed edge: cell %v route %v", s.cell, s.route[s.next:])
	}
	step(engine.Input{})
	order(2.5, 2.5) // This time it is the active edge's endpoint.
	for range scoutEdgeTicks {
		step(engine.Input{})
	}
	if s.cell != (grid.Cell{X: 2, Y: 2}) || s.next != len(s.route) || s.substep != 0 {
		t.Fatalf("scout did not stop at the requested center: %+v", s)
	}
	for _, item := range s.items {
		if item.ID == uint64(s.layout.Scout.ID) && (item.GroundX != 2.5 || item.GroundY != 2.5) {
			t.Fatalf("paint anchor did not land exactly at the center: %+v", item)
		}
	}
}

func TestAuthoredWallEditChangesTheScoutRoute(t *testing.T) {
	// Remove the middle wall tile without changing the command or spawn.
	const wall = `"....#...."`
	_, remainder, found := strings.Cut(authoredLevel, wall)
	if !found {
		t.Fatal("fixture no longer contains the wall to edit")
	}
	prefix := authoredLevel[:len(authoredLevel)-len(remainder)]
	edited := prefix + strings.Replace(remainder, wall, `"........."`, 1)
	for _, tc := range []struct {
		contents string
		want     grid.Cell
	}{
		{authoredLevel, grid.Cell{X: 3, Y: 3}},
		{edited, grid.Cell{X: 4, Y: 2}},
	} {
		layout, err := courtyardlevel.Decode(strings.NewReader(tc.contents))
		if err != nil {
			t.Fatal(err)
		}
		s := &authoredCourtyard{layout: layout}
		if err := s.Enter(); err != nil {
			t.Fatal(err)
		}
		tick := engine.Tick{Input: engine.Input{
			PrimaryPress:   authoredPress(s, 2.5, 2.5, 12),
			SecondaryPress: authoredPress(s, 6.5, 2.5, 0),
		}}
		if _, err := s.Update(tick); err != nil {
			t.Fatal(err)
		}
		for range 2*scoutEdgeTicks - 1 {
			if _, err := s.Update(engine.Tick{}); err != nil {
				t.Fatal(err)
			}
		}
		if s.cell != tc.want {
			t.Errorf("scout reached %v on its second edge; want %v", s.cell, tc.want)
		}
	}
}

func authoredPress(s *authoredCourtyard, x, y, elevation float64) engine.MousePress {
	px, py := s.camera.ToLogical(courtyardProjection.Project(x, y, elevation))
	return engine.MousePress{X: px, Y: py, Valid: true}
}

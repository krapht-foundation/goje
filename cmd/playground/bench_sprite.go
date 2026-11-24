//go:build goje_bench && !goje_smoke

package main

import (
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/sprite"
)

func spriteBenchmarkFactory(project authoredProject, count int, animated bool) engine.SceneFactory {
	return func() engine.Scene {
		return &spriteBenchmark{
			authoredCourtyard: &authoredCourtyard{layout: project.layout, idle: project.idle},
			actors:            make([]benchmarkActor, count),
			move:              project.move,
			animated:          animated,
		}
	}
}

type benchmarkActor struct {
	player sprite.Player
	x, y   float32
}

// Both modes draw the same authored ground and submit one quad per actor.
// Anchors stay fixed so pathfinding, movement, and sorting cannot skew the comparison.
type spriteBenchmark struct {
	*authoredCourtyard
	actors   []benchmarkActor
	move     sprite.Clip
	fixed    sprite.Frame
	animated bool
}

func (s *spriteBenchmark) Enter() error {
	if err := s.authoredCourtyard.Enter(); err != nil {
		return err
	}
	scout := s.layout.Scout
	px, py := courtyardProjection.Project(float64(scout.X)+0.5, float64(scout.Y)+0.5, 0)
	x, y := s.camera.ToLogical(px, py)
	for i := range s.actors {
		actor := &s.actors[i]
		actor.x, actor.y = float32(x), float32(y)
		if len(s.actors) > 1 {
			// Keep every dense actor in view, with increasing ground depth by row.
			actor.x, actor.y = 18+4.5*float32(i%64), 31+4.5*float32(i/64)
		}
		actor.player.Step(s.move)
	}
	s.fixed, _ = s.actors[0].player.Frame()
	return nil
}

func (s *spriteBenchmark) Update(engine.Tick) (engine.Transition, error) {
	if s.animated {
		for i := range s.actors {
			s.actors[i].player.Step(s.move)
		}
	}
	return engine.Transition{}, nil
}

func (s *spriteBenchmark) Draw(canvas engine.Canvas) {
	s.drawGround(canvas, 1)
	for i := range s.actors {
		actor := &s.actors[i]
		frame := s.fixed
		if s.animated {
			frame, _ = actor.player.Frame()
		}
		x, y, w, h := frame.Destination(actor.x, actor.y, 0.75)
		canvas.Image(frame.Source, x, y, w, h)
	}
}

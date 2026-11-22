package playground

import (
	"errors"
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
)

const smokeMoveTicks = 20

type nativeSmoke struct {
	*game
	ticks   int
	started bool
	passed  bool
	err     error
}

func runSmoke(g *game) error {
	ebiten.SetRunnableOnUnfocused(true)
	smoke := &nativeSmoke{game: g}
	if err := ebiten.RunGame(smoke); err != nil {
		return fmt.Errorf("run native smoke scenario: %w", err)
	}
	if !smoke.passed {
		return errors.New("window closed before the native smoke scenario finished")
	}
	return nil
}

func (s *nativeSmoke) Update() error {
	if s.err != nil {
		return s.err
	}
	if s.passed {
		return ebiten.Termination
	}
	if s.started && s.ticks < smokeMoveTicks {
		s.move(1, 0)
		s.ticks++
	}
	return nil
}

func (s *nativeSmoke) Draw(screen *ebiten.Image) {
	s.game.Draw(screen)
	switch {
	case !s.started:
		if !markerAt(screen, 160, 92) {
			s.err = errors.New("initial native frame did not contain the marker at (160, 92)")
		}
		s.started = true
	case s.ticks == smokeMoveTicks:
		if markerAt(screen, 160, 92) || !markerAt(screen, 240, 92) {
			s.err = errors.New("native frame did not show the marker moving from (160, 92) to (240, 92)")
			return
		}
		s.passed = true
	}
}

func markerAt(screen *ebiten.Image, x, y int) bool {
	r, g, b, a := screen.At(x, y).RGBA()
	mr, mg, mb, ma := markerColor.RGBA()
	return r == mr && g == mg && b == mb && a == ma
}

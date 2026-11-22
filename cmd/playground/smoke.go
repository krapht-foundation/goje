package main

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

const smokeMoveTicks = 20

type nativeSmoke struct {
	*game
	ticks   int
	started bool
	moved   bool
	reset   bool
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
	switch {
	case s.started && s.ticks < smokeMoveTicks:
		s.update(1, 0, false)
		s.ticks++
	case s.moved && !s.reset:
		s.update(0, 0, true)
		s.reset = true
	}
	return nil
}

func (s *nativeSmoke) Draw(screen *ebiten.Image) {
	s.game.Draw(screen)
	switch {
	case !s.started:
		if !atStart(screen) {
			s.err = errors.New("initial native frame did not show the player and token at spawn")
		}
		s.started = true
	case s.ticks == smokeMoveTicks && !s.moved:
		if !pixelIs(screen, 240, 92, markerColor) || pixelIs(screen, 192, 108, tokenColor) {
			s.err = errors.New("native frame did not show the player after collecting the token")
			return
		}
		s.moved = true
	case s.reset:
		if !atStart(screen) || pixelIs(screen, 240, 92, markerColor) {
			s.err = errors.New("native frame did not show the player and token restored after reset")
			return
		}
		s.passed = true
	}
}

func atStart(screen *ebiten.Image) bool {
	return pixelIs(screen, 160, 92, markerColor) && pixelIs(screen, 192, 108, tokenColor)
}

func pixelIs(screen *ebiten.Image, x, y int, want color.RGBA) bool {
	r, g, b, a := screen.At(x, y).RGBA()
	mr, mg, mb, ma := want.RGBA()
	return r == mr && g == mg && b == mb && a == ma
}

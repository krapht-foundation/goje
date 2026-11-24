//go:build goje_beacon_smoke && goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

type smokeStage uint8

const (
	smokeOpen smokeStage = iota
	smokeBlink
	smokeHop
	smokeToggle
	smokeReset
	smokeDone
)

func run(config platform.Config, factory func() *scene) error {
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	eye := color.NRGBA{R: 244, G: 241, B: 235, A: 255}
	red := color.NRGBA{R: 215, G: 84, B: 73, A: 255}
	ink := color.NRGBA{R: 36, G: 38, B: 46, A: 255}
	accent := color.NRGBA{R: 68, G: 174, B: 145, A: 255}
	stage, issued := smokeOpen, false
	ticks := 0
	var current *scene
	// Advance from actual framebuffer pixels, not the state that requested a draw.
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		pixel := func(x, y int, want color.NRGBA) bool {
			return desktop.ProbePixel(viewport, image.Pt(width, height), image.Pt(x, y)) == want
		}
		board := func(beacon color.NRGBA) bool {
			return pixel(64, 32, beacon) && pixel(32, 32, floor) &&
				pixel(24, 25, accent) && pixel(64, 64, wall)
		}
		standing := func(x int) bool {
			return pixel(x-7, startY-9, eye) && pixel(x, startY-1, ink)
		}
		var matched bool
		switch stage {
		case smokeOpen:
			matched = board(dormant) && standing(startX)
		case smokeBlink:
			// Wait for the first possible blink instead of stalling on GL readback every idle frame.
			matched = ticks >= 114 && pixel(startX-7, startY-9, red) && pixel(startX, startY-1, ink)
		case smokeHop:
			x := current.x
			matched = x > startX && pixel(x-7, startY-13, eye) && pixel(x, startY-1, background)
		case smokeToggle:
			matched = issued && board(lit)
		case smokeReset:
			matched = issued && board(dormant) && standing(startX)
		case smokeDone:
		}
		if matched {
			stage++
			issued = false
			if stage == smokeDone {
				cancel()
			}
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, config, func() engine.Scene {
		current = factory()
		return &smokeScene{Scene: current, input: func() engine.Input {
			ticks++
			switch {
			case stage == smokeHop:
				return engine.Input{Held: engine.KeyArrowRight}
			case stage == smokeToggle && !issued:
				issued = true
				return engine.Input{Pressed: engine.KeySpace}
			case stage == smokeReset && !issued:
				issued = true
				return engine.Input{Pressed: engine.KeyR}
			}
			return engine.Input{}
		}}
	}); err != nil {
		return fmt.Errorf("run native Beacon smoke: %w", err)
	}
	if stage != smokeDone {
		return fmt.Errorf("native Beacon smoke ended at checkpoint %d after %d ticks", stage, ticks)
	}
	slog.Info(
		"Native Beacon smoke passed: editor assets, tomato blink and hop, beacon toggle, and reset",
	)
	return nil
}

type smokeScene struct {
	engine.Scene
	input func() engine.Input
}

func (s *smokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = s.input()
	return s.Scene.Update(tick)
}

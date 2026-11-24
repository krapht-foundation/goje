//go:build goje_beacon_smoke && goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"os"
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
	if len(os.Args) == 2 && os.Args[1] == "--sprite" {
		return runSprite(config, factory)
	}
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

// The Sprite Clip Editor export orders the move clip hop (16 ticks), windup,
// standing. The original clip had landed by motion tick 13; the export is
// still airborne there and plants on the windup at motion tick 17.
const spriteLanding = 17

// runSprite plays the exported clip with the repository arena and floor.
func runSprite(config platform.Config, factory func() *scene) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	eye := color.NRGBA{R: 244, G: 241, B: 235, A: 255}
	ink := color.NRGBA{R: 36, G: 38, B: 46, A: 255}
	stage, motion, probed, issued := "standing spawn", 0, 0, false
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		pixel := func(x, y int, want color.NRGBA) bool {
			return desktop.ProbePixel(viewport, image.Pt(width, height), image.Pt(x, y)) == want
		}
		spawn := func() bool {
			return pixel(64, 64, wall) && pixel(startX-7, startY-9, eye) &&
				pixel(startX, startY-1, ink)
		}
		switch stage {
		case "standing spawn":
			if spawn() {
				stage = "movement"
			}
		case "movement":
			if probed == motion {
				return nil
			}
			probed = motion
			x := startX + motion
			var name string
			var ok bool
			switch motion {
			case 13, 16:
				name = "airborne hop"
				ok = pixel(x-7, startY-13, eye) && pixel(x, startY-1, background) &&
					pixel(x, startY-2, background)
			case spriteLanding:
				name = "planted windup"
				ok = pixel(x-5, startY-6, eye) && pixel(x, startY-1, ink) && pixel(x, startY-2, ink)
			default:
				return nil
			}
			if !ok {
				return fmt.Errorf("native Beacon sprite smoke: %s pixels missing on motion tick %d",
					name, motion)
			}
			if motion == spriteLanding {
				stage = "reset"
			}
		case "reset":
			if !issued {
				return nil
			}
			x := startX + spriteLanding
			if !spawn() || !pixel(x, startY-1, background) || !pixel(x-5, startY-6, background) {
				return errors.New(
					"native Beacon sprite smoke: reset did not restore the standing spawn")
			}
			stage = "done"
			cancel()
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, config, func() engine.Scene {
		return &gatedScene{Scene: factory(), input: func() (engine.Input, bool) {
			switch {
			case stage == "movement" && motion > probed:
				return engine.Input{}, false
			case stage == "movement":
				motion++
				return engine.Input{Held: engine.KeyArrowRight}, true
			case stage == "reset" && !issued:
				issued = true
				return engine.Input{Pressed: engine.KeyR}, true
			}
			return engine.Input{}, true
		}}
	}); err != nil {
		return fmt.Errorf("run native Beacon sprite smoke: %w", err)
	}
	if stage != "done" {
		return fmt.Errorf("native Beacon sprite smoke ended at %s checkpoint after motion tick %d",
			stage, motion)
	}
	slog.Info("Native Beacon sprite smoke passed: exported hop airborne on motion ticks " +
		"13 and 16, planted on tick 17, and reset")
	return nil
}

// gatedScene skips the real Update while input holds the simulation, so every
// movement tick reaches the back buffer however far the host falls behind.
type gatedScene struct {
	engine.Scene
	input func() (engine.Input, bool)
}

func (s *gatedScene) Update(tick engine.Tick) (engine.Transition, error) {
	input, step := s.input()
	if !step {
		return engine.Transition{}, nil
	}
	tick.Input = input
	return s.Scene.Update(tick)
}

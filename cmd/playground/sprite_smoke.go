//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

// The Sprite Clip Editor export orders the move clip hop (16 ticks), windup,
// standing. The original clip had landed by motion tick 13; the export is
// still airborne there and plants on the windup at motion tick 17.
const spriteLanding = 17

// gatedSmokeScene skips the real Update while input holds the simulation, so
// every movement tick reaches the back buffer however far the host falls behind.
type gatedSmokeScene struct {
	engine.Scene
	input func() (engine.Input, bool)
}

func (s *gatedSmokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	input, step := s.input()
	if !step {
		return engine.Transition{}, nil
	}
	tick.Input = input
	return s.Scene.Update(tick)
}

func runSpriteSmoke() error {
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	groundAccent := color.NRGBA{R: 91, G: 133, B: 114, A: 255}
	tomato := color.NRGBA{R: 215, G: 84, B: 73, A: 255}
	ink := color.NRGBA{R: 36, G: 38, B: 46, A: 255}
	eye := color.NRGBA{R: 244, G: 241, B: 235, A: 255}
	spawn := func(v image.Rectangle) bool {
		return pixelIs(v, 144, 71, tomato) && pixelIs(v, 139, 70, eye) &&
			pixelIs(v, 176, 80, courtyardWallColor) && pixelIs(v, 141, 41, groundAccent) &&
			!pixelIs(v, 144, 53, selectionColor) && !pixelIs(v, 96, 100, destinationColor)
	}
	stage, motion, probed, issued := "authored spawn", 0, 0, false
	input := func() (engine.Input, bool) {
		switch {
		case stage == "movement" && motion > probed:
			return engine.Input{}, false
		case stage == "movement":
			motion++
			if motion == 1 {
				// Select the scout and send it straight along +board Y from (2,2) to (2,5).
				return engine.Input{
					PrimaryPress:   engine.MousePress{X: 144, Y: 64, Valid: true},
					SecondaryPress: engine.MousePress{X: 96, Y: 100, Valid: true},
				}, true
			}
		case stage == "reset" && !issued:
			issued = true
			return engine.Input{Pressed: engine.KeyR}, true
		}
		return engine.Input{}, true
	}
	desktop.FrameProbe = func(v image.Rectangle) error {
		switch stage {
		case "authored spawn":
			if spawn(v) {
				stage = "movement"
			}
		case "movement":
			if probed == motion {
				return nil
			}
			probed = motion
			// The ground anchor moves one pixel left and half a pixel down per tick.
			x, y := 144-motion, 76+motion/2
			var name string
			var ok bool
			switch motion {
			case 1:
				name, ok = "route to (2,5)", pixelIs(v, 96, 100, destinationColor)
			case 13, 16:
				name = "airborne hop"
				ok = pixelIs(v, x, y-7, tomato) && pixelIs(v, x, y-4, ink) &&
					!pixelIs(v, x, y-2, ink) && !pixelIs(v, x, y-1, ink)
			case spriteLanding:
				name = "planted windup"
				// Sample the planted body away from the fractional-width sprite edge.
				ok = pixelIs(v, x, y-1, ink) && pixelIs(v, x, y-4, tomato)
			default:
				return nil
			}
			if !ok {
				return fmt.Errorf("native sprite smoke: %s pixels missing on motion tick %d",
					name, motion)
			}
			if motion == spriteLanding {
				stage = "reset"
			}
		case "reset":
			if !issued {
				return nil
			}
			if !spawn(v) || pixelIs(v, 127, 83, ink) || pixelIs(v, 122, 78, eye) {
				return errors.New("native sprite smoke: reset did not restore the authored spawn")
			}
			stage = "done"
			cancel()
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	factory := loaded.factory()
	if err := platform.Run(ctx, loaded.config(), func() engine.Scene {
		return &gatedSmokeScene{Scene: factory(), input: input}
	}); err != nil {
		return fmt.Errorf("native sprite smoke: %w", err)
	}
	if stage != "done" {
		return fmt.Errorf("native sprite smoke ended at %s checkpoint after motion tick %d",
			stage, motion)
	}
	return nil
}

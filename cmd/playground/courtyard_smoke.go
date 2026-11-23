//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

// A checkpoint advances only after the scripted tick's result reaches the
// OpenGL back buffer. Extra catch-up ticks receive neutral input.
type courtyardCheckpoint struct {
	name  string
	input engine.Input
	check func(image.Rectangle) bool
}

func courtyardCheckpoints() []courtyardCheckpoint {
	click := func(x, y float64) engine.Input {
		return engine.Input{PrimaryPress: engine.MousePress{X: x, Y: y, Valid: true}}
	}
	return []courtyardCheckpoint{
		{name: "spawn overlap", check: courtyardSpawn},
		{name: "front unit selected", input: click(160, 88), check: func(v image.Rectangle) bool {
			return pixelIs(v, 160, 81, selectionColor)
		}},
		{name: "wall blocks rear unit", input: click(160, 84), check: courtyardSpawn},
		{
			name:  "exposed rear unit selected",
			input: click(155, 84),
			check: func(v image.Rectangle) bool {
				return pixelIs(v, 160, 77, selectionColor)
			},
		},
		{
			name:  "camera pan and zoom",
			input: engine.Input{Held: engine.KeyArrowRight, Pressed: engine.KeyW},
			check: func(v image.Rectangle) bool {
				return pixelIs(v, 158, 72, selectionColor) && !pixelIs(v, 164, 72, selectionColor) &&
					pixelIs(v, 158, 84, tokenColor) && pixelIs(v, 162, 79, courtyardWallColor)
			},
		},
		{name: "resize preserves selection with black bars", check: func(v image.Rectangle) bool {
			return pixelIs(v, 158, 72, selectionColor) && pixelIs(v, -8, 100, color.NRGBA{A: 255})
		}},
		{
			name:  "reset restores camera, units, and selection",
			input: engine.Input{Pressed: engine.KeyR},
			check: func(v image.Rectangle) bool {
				return courtyardSpawn(v) && !pixelIs(v, 158, 72, selectionColor)
			},
		},
	}
}

func courtyardSpawn(v image.Rectangle) bool {
	return pixelIs(v, 160, 88, tokenColor) && pixelIs(v, 160, 84, courtyardWallColor) &&
		pixelIs(v, 155, 84, markerColor) && pixelIs(v, 160, 81, courtyardWallColor)
}

type courtyardSmokeScene struct {
	engine.Scene
	input func() engine.Input
}

func (s *courtyardSmokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = s.input()
	return s.Scene.Update(tick)
}

func runCourtyardSmoke() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks := courtyardCheckpoints()
	index, issued := 0, false
	input := func() engine.Input {
		if index >= len(checks) || issued {
			return engine.Input{}
		}
		issued = true
		return checks[index].input
	}
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		if index >= len(checks) {
			return nil
		}
		check := checks[index]
		if check.input != (engine.Input{}) && !issued {
			return nil
		}
		const resizeCheckpoint = 5
		if index == resizeCheckpoint && viewport.Min.X == 0 {
			return nil // Wait for the GLFW framebuffer size callback.
		}
		if !check.check(viewport) {
			return fmt.Errorf("courtyard pixels did not match checkpoint: %s", check.name)
		}
		index, issued = index+1, false
		if index == resizeCheckpoint {
			glfw.GetCurrentContext().SetSize(880, 400)
		}
		if index == len(checks) {
			cancel()
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, playgroundConfig(), func() engine.Scene {
		return &courtyardSmokeScene{Scene: newCourtyard(), input: input}
	}); err != nil {
		return fmt.Errorf("native courtyard smoke: %w", err)
	}
	if index != len(checks) {
		return errors.New("window closed before the courtyard smoke scenario finished")
	}
	return nil
}

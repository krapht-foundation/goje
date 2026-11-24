//go:build goje_beacon_smoke && goje_smoke && cgo && (darwin || linux)

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"os"
	"sync/atomic"
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
	if len(os.Args) == 2 && os.Args[1] == "--audio" {
		return runAudio(config, factory)
	}
	config.Sounds = nil
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

// runAudio watches real device callbacks without requiring speakers in CI.
func runAudio(config platform.Config, factory func() *scene) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clip := config.Sounds[toggleCue].PCM()
	offset := firstSignal(clip)
	start := time.Now()
	var requested, received atomic.Int64
	var complete, wrong atomic.Bool
	desktop.AudioNull = os.Getenv("GOJE_AUDIO_DEVICE") != "default"
	desktop.AudioProbe = func(pcm []byte) {
		if received.Load() == 0 {
			pcm = pcm[firstSignal(pcm):]
			if len(pcm) == 0 {
				return
			}
			received.Store(int64(time.Since(start)))
		}
		n := min(len(pcm), len(clip)-offset)
		if !bytes.Equal(pcm[:n], clip[offset:offset+n]) || firstSignal(pcm[n:]) < len(pcm)-n {
			wrong.Store(true)
		}
		offset += n
		if offset == len(clip) {
			complete.Store(true)
		}
	}
	// Keep the runtime running under headless window managers. The callback
	// acknowledges the buffer independently of how fast the window presents.
	desktop.FrameProbe = func(image.Rectangle) error { return nil }
	defer func() {
		desktop.AudioProbe, desktop.FrameProbe, desktop.AudioNull = nil, nil, false
	}()

	var current *scene
	resetAt := time.Duration(0)
	if err := platform.Run(ctx, config, func() engine.Scene {
		current = factory()
		return &smokeScene{Scene: current, input: func() engine.Input {
			switch {
			case requested.Load() == 0:
				requested.Store(int64(time.Since(start)))
				return engine.Input{
					Pressed: engine.KeySpace,
					PrimaryPress: engine.MousePress{
						X: float64(left + current.beacon.X*cell + 1),
						Y: float64(top + current.beacon.Y*cell + 1), Valid: true,
					},
				}
			case complete.Load() && resetAt == 0:
				if !current.on {
					wrong.Store(true)
				}
				resetAt = time.Since(start)
				return engine.Input{Pressed: engine.KeyR}
			case resetAt != 0 && time.Since(start)-resetAt > 150*time.Millisecond:
				cancel()
			}
			return engine.Input{}
		}}
	}); err != nil {
		return fmt.Errorf("run native audio smoke: %w", err)
	}
	if !complete.Load() || wrong.Load() || resetAt == 0 || current.on {
		return errors.New("native audio smoke: expected one exact cue and silence after reset")
	}
	slog.Info("Beacon audio smoke passed", "null_device", desktop.AudioNull,
		"request_to_callback", time.Duration(received.Load()-requested.Load()))
	return nil
}

func firstSignal(pcm []byte) int {
	for i := 0; i < len(pcm); i += 2 {
		if pcm[i] != 0 || pcm[i+1] != 0 {
			return i
		}
	}
	return len(pcm)
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
	cyan := color.NRGBA{R: 22, G: 216, B: 255, A: 255}
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
				name = "painted airborne hop"
				// The painted stripe crosses the hop center 10 pixels above the ground.
				ok = pixel(x, startY-10, cyan) && pixel(x-7, startY-13, eye) &&
					pixel(x, startY-1, background) && pixel(x, startY-2, background)
			case spriteLanding:
				name = "planted windup"
				ok = !pixel(x, startY-10, cyan) && pixel(x-5, startY-6, eye) &&
					pixel(x, startY-1, ink) && pixel(x, startY-2, ink)
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
	slog.Info("Native Beacon sprite smoke passed: exported hop airborne with its painted cyan " +
		"stripe on motion ticks 13 and 16, planted on tick 17, and reset")
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

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
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

type smokeStage uint8

const (
	smokeOpen smokeStage = iota
	smokeBlink
	smokeHop
	smokeTap
	smokeReset
	smokeWin
	smokeAgain
	smokeTimeout
	smokeTapAgain
	smokeDone
)

// smokeNames label the checkpoints in failure messages.
var smokeNames = [...]string{
	"open", "blink", "hop", "tap", "reset", "keyboard win", "Space restart", "timeout",
	"tap restart", "done",
}

// turbo is how many game ticks the timeout checkpoint runs per host tick, so
// the fixed 600-tick timer expires in about a second of wall time.
const turbo = 10

func run(config platform.Config, factory func() *scene) error {
	if len(os.Args) == 2 && os.Args[1] == "--audio" {
		return runAudio(config, factory)
	}
	config.Sounds, config.Music = nil, nil
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
		// The editor-authored floor, tile paint, and wall, and every beacon's center.
		board := func(tint func(i int) color.NRGBA) bool {
			for i, b := range current.beacons {
				if !pixel(32+b.X*cell, 32+b.Y*cell, tint(i)) {
					return false
				}
			}
			return pixel(32, 32, floor) && pixel(24, 25, accent) && pixel(64, 64, wall)
		}
		all := func(c color.NRGBA) func(int) color.NRGBA { return func(int) color.NRGBA { return c } }
		// A fresh round: unlit beacons, a full timer bar, and no result banner.
		fresh := func() bool {
			return board(all(dormant)) && pixel(width-left-2, barY+1, timeLeft) &&
				pixel(left+1, bannerY+1, background)
		}
		standing := func(x int) bool {
			return pixel(x-7, startY-9, eye) && pixel(x, startY-1, ink)
		}
		var matched bool
		switch stage {
		case smokeOpen:
			matched = fresh() && standing(startX)
		case smokeBlink:
			// Wait for the first possible blink instead of stalling on GL readback every idle frame.
			matched = ticks >= 114 && pixel(startX-7, startY-9, red) && pixel(startX, startY-1, ink)
		case smokeHop:
			x := current.x
			matched = x > startX && pixel(x-7, startY-13, eye) && pixel(x, startY-1, background)
		case smokeTap:
			last := len(current.beacons) - 1
			matched = issued && board(func(i int) color.NRGBA {
				if i == last {
					return lit
				}
				return dormant
			})
		case smokeReset, smokeAgain:
			matched = issued && fresh() && standing(startX)
		case smokeWin:
			matched = current.phase == won && board(all(lit)) && pixel(left+1, bannerY+1, lit)
		case smokeTimeout:
			matched = current.phase == timedOut && pixel(left+1, bannerY+1, timeUp)
		case smokeTapAgain:
			matched = issued && fresh()
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
			case stage == smokeTap && !issued:
				issued = true
				return tapBeacon(current, len(current.beacons)-1)
			case stage == smokeReset && !issued:
				issued = true
				return engine.Input{Pressed: engine.KeyR}
			case stage == smokeWin && current.phase <= playing:
				// Walk right and press Space under every column that still has an unlit beacon.
				for i, b := range current.beacons {
					if !current.lit[i] && b.X == (current.x-left)/cell {
						return engine.Input{Pressed: engine.KeySpace}
					}
				}
				return engine.Input{Held: engine.KeyArrowRight}
			case stage == smokeAgain && !issued:
				issued = true
				return engine.Input{Pressed: engine.KeySpace}
			case stage == smokeTimeout && current.phase == ready:
				// One step starts the clock without lighting anything.
				return engine.Input{Held: engine.KeyArrowRight}
			case stage == smokeTapAgain && !issued:
				issued = true
				// The lane is not a beacon; any tap restarts a finished round.
				return engine.Input{PrimaryPress: engine.MousePress{X: 80, Y: 100, Valid: true}}
			}
			return engine.Input{}
		}, steps: func() int {
			if stage == smokeTimeout && current.phase == playing {
				return turbo
			}
			return 1
		}}
	}); err != nil {
		return fmt.Errorf("run native Beacon smoke: %w", err)
	}
	if stage != smokeDone {
		return fmt.Errorf("native Beacon smoke ended at the %s checkpoint after %d ticks",
			smokeNames[stage], ticks)
	}
	slog.Info("Native Beacon smoke passed: editor assets, tomato blink and hop, pointer lighting, " +
		"reset, keyboard win, Space restart, timeout, and tap restart")
	return nil
}

// tapBeacon is a primary press at the center of beacon i's cell.
func tapBeacon(s *scene, i int) engine.Input {
	b := s.beacons[i]
	return engine.Input{PrimaryPress: engine.MousePress{
		X: float64(left + b.X*cell + cell/2), Y: float64(top + b.Y*cell + cell/2), Valid: true,
	}}
}

// runAudio watches real device callbacks without requiring speakers in CI. It
// keeps a reference mixer in step with the device: the host applies requests
// only between buffers, so each buffer must equal the reference rendered after
// some prefix of the requests the game is expected to make. Beacon's centered
// cue overlaps a quieter copy panned right, which the smoke requests in the
// same tick, under the music that fades in on the first update, fades down
// when the round is won, and fades back up on restart without restarting.
func runAudio(config platform.Config, factory func() *scene) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const overlapGain, overlapPan = 0.5, 1
	cue, music := config.Sounds[toggleCue], config.Music[musicTrack]
	start := time.Now()
	var (
		mu        sync.Mutex
		reference audio.Mixer
		pending   []func(*audio.Mixer)
		scratch   []float32
		received  time.Duration // when the first request was heard
	)
	var wrong atomic.Bool
	expect := func(requests ...func(*audio.Mixer)) {
		mu.Lock()
		pending = append(pending, requests...)
		mu.Unlock()
	}
	settled := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(pending) == 0
	}
	desktop.AudioNull = os.Getenv("GOJE_AUDIO_DEVICE") != "default"
	desktop.AudioProbe = func(frames []float32) {
		mu.Lock()
		defer mu.Unlock()
		if cap(scratch) < len(frames) {
			scratch = make([]float32, len(frames))
		}
		out := scratch[:len(frames)]
		// Try the longest prefix first; the music is audible from its second
		// sample, so a buffer cannot match while hiding a request that played.
		for k := len(pending); k >= 0; k-- {
			try := reference
			for _, request := range pending[:k] {
				request(&try)
			}
			try.Render(out)
			if slices.Equal(out, frames) {
				if k > 0 && received == 0 {
					received = time.Since(start)
				}
				reference, pending = try, pending[k:]
				return
			}
		}
		wrong.Store(true)
	}
	// Keep the runtime running under headless window managers. The callback
	// acknowledges the buffer independently of how fast the window presents.
	desktop.FrameProbe = func(image.Rectangle) error { return nil }
	defer func() {
		desktop.AudioProbe, desktop.FrameProbe, desktop.AudioNull = nil, nil, false
	}()

	playMusic := func(m *audio.Mixer) { m.PlayMusic(music, musicGain, musicFadeMS) }
	playCue := func(m *audio.Mixer) { m.Start(cue, 1, 0) }
	var current *scene
	stage, since := 0, time.Duration(0)
	var requested time.Duration
	next := func() { stage, since = stage+1, time.Since(start) }
	if err := platform.Run(ctx, config, func() engine.Scene {
		current = factory()
		return &smokeScene{Scene: current, input: func() engine.Input {
			waited := time.Since(start) - since
			switch {
			case stage == 0:
				// Space lights the column above the tomato and the press lights
				// the first beacon; together they make one cue.
				requested = time.Since(start)
				expect(playMusic, playCue,
					func(m *audio.Mixer) { m.Start(cue, overlapGain, overlapPan) })
				next()
				input := tapBeacon(current, 0)
				input.Pressed = engine.KeySpace
				return input
			case stage == 1 && settled() && waited > 400*time.Millisecond:
				for i := range current.beacons {
					if current.lit[i] {
						continue
					}
					if current.unlit == 1 {
						expect(playCue,
							func(m *audio.Mixer) { m.SetMusicGain(finishGain, musicFadeMS) })
						next()
					} else {
						expect(playCue)
					}
					return tapBeacon(current, i)
				}
			case stage == 2 && settled() && waited > 700*time.Millisecond:
				if current.phase != won {
					wrong.Store(true)
				}
				// The restarted round requests the same track, which only fades back up.
				expect(playMusic)
				next()
				return engine.Input{Pressed: engine.KeyR}
			case stage == 3 && settled() && waited > 700*time.Millisecond:
				next()
				cancel()
			}
			return engine.Input{}
		}, after: func(tick engine.Tick) {
			// A device callback could drain the queue between Beacon's request
			// and this one; the prefix match accepts that split.
			if tick.Input.Pressed&engine.KeySpace != 0 {
				tick.PlaySoundAt(toggleCue, overlapGain, overlapPan)
			}
		}}
	}); err != nil {
		return fmt.Errorf("run native audio smoke: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if stage != 4 || wrong.Load() || len(pending) > 0 || current.phase != ready {
		return fmt.Errorf("native audio smoke: stage %d, mismatch %t, %d requests unheard, "+
			"phase %d; expected exactly mixed cues and music that fades down on the win "+
			"and back up on restart", stage, wrong.Load(), len(pending), current.phase)
	}
	if received == 0 {
		return errors.New("native audio smoke: the device never played a request")
	}
	slog.Info("Beacon audio smoke passed", "null_device", desktop.AudioNull,
		"request_to_callback", received-requested)
	return nil
}

type smokeScene struct {
	engine.Scene
	input func() engine.Input
	after func(engine.Tick) // optional, runs after the scene's update
	steps func() int        // optional, game updates per host tick
}

func (s *smokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = s.input()
	steps := 1
	if s.steps != nil {
		steps = s.steps()
	}
	transition, err := s.Scene.Update(tick)
	// Fast-forward with idle input; the timer only reaches its limit while playing.
	idle := tick
	idle.Input = engine.Input{}
	for range steps - 1 {
		if err != nil {
			break
		}
		transition, err = s.Scene.Update(idle)
	}
	if s.after != nil {
		s.after(tick)
	}
	return transition, err
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

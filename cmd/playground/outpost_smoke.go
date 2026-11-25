//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
	"github.com/krapht-foundation/goje/ui"
)

// smokeStore keeps the smoke's saves and recordings in memory, so the smoke
// never writes to the user's configuration directory.
type smokeStore map[string][]byte

func (m smokeStore) Put(key string, value []byte) error { m[key] = slices.Clone(value); return nil }

func (m smokeStore) Get(key string) ([]byte, error) {
	if v, ok := m[key]; ok {
		return slices.Clone(v), nil
	}
	return nil, platform.ErrNotFound
}

func (m smokeStore) Delete(key string) error { delete(m, key); return nil }

// The stages of the outpost smoke, in order. Each stage sends its inputs, one
// per tick, and then inspects the frame that follows.
const (
	outpostStageTitle = iota
	outpostStageStart
	outpostStageBox
	outpostStageMove
	outpostStageFight
	outpostStageRestart
	outpostStageDragPress
	outpostStageDragMove
	outpostStageDragRelease
	outpostStageHold
	outpostStagePause
	outpostStageSave
	outpostStageResume
	outpostStageRun
	outpostStageLoad
	outpostStageQuit
	outpostStageWatch
	outpostStageReplay
	outpostStageDone
)

// outpostFightTicks bounds the fight; the recorded one ends at about 415 ticks.
const outpostFightTicks = 700

// outpostSmoke sends real presses through the app's normal Update. Like the
// skirmish script it runs one tick per presented frame, so every state change is
// seen on a frame before the next tick can change it. The first match plays the
// recorded fight of TestOutpostRecordedMatchWinsAndReplays.
type outpostSmoke struct {
	app     *outpostApp
	store   smokeStore
	stage   int
	waiting bool           // A tick ran whose frame the probe has not inspected yet.
	pending []engine.Input // Inputs of this stage not sent yet.
	started bool           // The stage's inputs were queued.

	fightTicks         int
	alarmLit, boltSeen bool
	whiteBefore        int // White pixels on the previous frame, for text that appears or grows.
	whiteHold          int
	pausedAt           uint64 // The tick count when the game paused.
	savedAt            uint64 // The tick count of the save.
}

type outpostSmokeScene struct {
	engine.Scene
	smoke *outpostSmoke
}

func (s *outpostSmokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	if s.smoke.waiting {
		return engine.Transition{}, nil
	}
	s.smoke.waiting = true
	tick.Input = s.smoke.input()
	return s.Scene.Update(tick)
}

func press(x, y float64) engine.MousePress { return engine.MousePress{X: x, Y: y, Valid: true} }

func keys(k ...engine.Keys) []engine.Input {
	inputs := make([]engine.Input, len(k))
	for i, key := range k {
		inputs[i].Pressed = key
	}
	return inputs
}

// screen converts a ground position and a height to whole logical pixels. The
// board's tile centers project onto whole pixels at zoom 1.
func (k *outpostSmoke) screen(x, y, elevation float64) (screenX, screenY int) {
	px, py := k.app.session.camera.ToLogical(courtyardProjection.Project(x, y, elevation))
	return int(math.Round(px)), int(math.Round(py))
}

// queue returns what the stage sends, once.
func (k *outpostSmoke) queue() []engine.Input {
	switch k.stage {
	case outpostStageStart:
		return keys(engine.KeyEnter)
	case outpostStageBox:
		return []engine.Input{
			{
				PrimaryPress: press(0, 0),
				PrimaryHeld:  true,
				Pointer:      engine.Pointer{X: 0, Y: 0, Inside: true},
			},
			{
				PrimaryRelease: press(width-1, height-1),
				Pointer:        engine.Pointer{X: width - 1, Y: height - 1, Inside: true},
			},
		}
	case outpostStageMove:
		x, y := k.screen(3.5, 4.5, 0)
		return []engine.Input{{SecondaryPress: press(float64(x), float64(y))}}
	case outpostStageRestart:
		return keys(engine.KeyR)
	case outpostStageDragPress:
		return []engine.Input{
			{
				PrimaryPress: press(1, 13),
				PrimaryHeld:  true,
				Pointer:      engine.Pointer{X: 1, Y: 13, Inside: true},
			},
		}
	case outpostStageDragMove:
		return []engine.Input{
			{PrimaryHeld: true, Pointer: engine.Pointer{X: 318, Y: 178, Inside: true}},
		}
	case outpostStageDragRelease:
		return []engine.Input{{
			PrimaryRelease: press(318, 178), Pointer: engine.Pointer{X: 318, Y: 178, Inside: true},
		}}
	case outpostStageHold:
		return keys(engine.KeyH)
	case outpostStagePause:
		k.pausedAt = k.app.session.sim.Ticks()
		return append(keys(engine.KeyEscape), engine.Input{}, engine.Input{})
	case outpostStageSave:
		return keys(engine.KeyArrowDown, engine.KeyEnter)
	case outpostStageResume:
		return keys(engine.KeyEscape)
	case outpostStageRun:
		return make([]engine.Input, 30)
	case outpostStageLoad:
		return keys(engine.KeyEscape, engine.KeyArrowDown, engine.KeyArrowDown, engine.KeyEnter)
	case outpostStageQuit:
		return keys(engine.KeyEscape, engine.KeyArrowUp, engine.KeyEnter)
	case outpostStageWatch:
		return keys(engine.KeyArrowDown, engine.KeyArrowDown, engine.KeyEnter)
	}
	return nil
}

func (k *outpostSmoke) input() engine.Input {
	if k.stage == outpostStageFight {
		// The same sequence as the recorded script: a focus order every second.
		n := k.fightTicks
		k.fightTicks++
		if n > 60 && n%60 == 0 {
			return engine.Input{Pressed: engine.KeyF}
		}
		return engine.Input{}
	}
	if !k.started {
		k.started = true
		k.pending = k.queue()
	}
	if len(k.pending) == 0 {
		return engine.Input{}
	}
	in := k.pending[0]
	k.pending = k.pending[1:]
	return in
}

// white counts the pixels of the default text color, which only glyphs use.
func white(viewport image.Rectangle) int { return countPixels(viewport, ui.DefaultStyle().Text) }

// checkMarkers checks that the frame marks every scout in the selection.
func (k *outpostSmoke) checkMarkers(viewport image.Rectangle, want int) error {
	s := k.app.session
	if len(s.selection) != want {
		return fmt.Errorf("selection holds %d scouts, want %d", len(s.selection), want)
	}
	for _, id := range s.selection {
		u, _ := s.sim.Unit(id)
		c := u.Center()
		x, y := k.screen(c.X.Float64(), c.Y.Float64(), 0)
		if !pixelIs(viewport, x, y-skirmishUnitHeight-7, selectionColor) {
			return fmt.Errorf("frame did not show the selection marker over scout %d", id)
		}
	}
	return nil
}

// checkSpawn checks that the frame shows all twelve units with full health bars.
func (k *outpostSmoke) checkSpawn(viewport image.Rectangle) error {
	s := k.app.session
	if s.sim.Ticks() != 0 || s.outcome != outcomePlaying {
		return errors.New("the match is not at its start")
	}
	for _, u := range s.sim.Units() {
		c := u.Center()
		x, y := k.screen(c.X.Float64(), c.Y.Float64(), 0)
		want := skirmishGuardColor
		if u.Team == skirmish.Scouts {
			want = skirmishScoutColor
		}
		barTop := y - skirmishUnitHeight - 3
		if !pixelIs(viewport, x, y-7, want) || !pixelIs(viewport, x-5, barTop, skirmishHealthColor) {
			return fmt.Errorf("frame did not show unit %d with a full health bar", u.ID)
		}
	}
	if countPixels(viewport, selectionColor) != 0 || countPixels(viewport, skirmishAlarmLoud) != 0 {
		return errors.New("frame showed a selection or a loud alarm at the start")
	}
	return nil
}

// checkFight inspects every frame of the fight. It completes when the scouts win.
func (k *outpostSmoke) checkFight(viewport image.Rectangle) (bool, error) {
	s := k.app.session
	if s.sim.Ticks() > outpostFightTicks {
		return false, errors.New("the fight did not end")
	}
	if countPixels(viewport, skirmishAlarmLoud) != 0 {
		k.alarmLit = true
	}
	if bolts := s.sim.Bolts(); len(bolts) > 0 && !k.boltSeen {
		fp := bolts[0].Body.Footprint
		x, y := k.screen(fp.Min.X.Add((fp.Max.X-fp.Min.X)/2).Float64(),
			fp.Min.Y.Add((fp.Max.Y-fp.Min.Y)/2).Float64(), float64(bolts[0].Body.Vertical.Bottom))
		if !pixelIs(viewport, x, y, skirmishBoltColor) {
			return false, fmt.Errorf("frame did not show the bolt at %d, %d", x, y)
		}
		k.boltSeen = true
	}
	now := white(viewport)
	before := k.whiteBefore
	k.whiteBefore = now
	if s.outcome != outcomeVictory {
		return false, nil
	}
	if !k.alarmLit || !k.boltSeen {
		return false, fmt.Errorf(
			"the fight ended without the alarm (%v) and a bolt (%v)",
			k.alarmLit,
			k.boltSeen,
		)
	}
	if now < before+150 {
		return false, fmt.Errorf(
			"frame did not show the victory text (%d white pixels after %d)",
			now,
			before,
		)
	}
	return true, nil
}

// check validates the frame just drawn and reports whether the stage is complete.
func (k *outpostSmoke) check(viewport image.Rectangle) (bool, error) {
	app, s := k.app, k.app.session
	if k.stage != outpostStageTitle && k.stage != outpostStageFight &&
		(!k.started || len(k.pending) != 0) {
		return false, nil // Wait for the stage's inputs to go out.
	}
	switch k.stage {
	case outpostStageTitle:
		if app.state != stateTitle || white(viewport) < 100 {
			return false, errors.New("frame did not show the title text")
		}
		return true, nil
	case outpostStageStart:
		if !k.started || app.state != statePlaying || s == nil {
			return false, nil
		}
		return true, k.checkSpawn(viewport)
	case outpostStageBox:
		if !k.started || s.sim.Ticks() < 2 {
			return false, nil
		}
		return true, k.checkMarkers(viewport, 6)
	case outpostStageMove:
		if !k.started {
			return false, nil
		}
		u, _ := s.sim.Unit(1)
		if _, moving := s.sim.Goal(u); !moving || countPixels(viewport, destinationColor) == 0 {
			return false, errors.New("frame did not show the destination markers after the move order")
		}
		return true, nil
	case outpostStageFight:
		return k.checkFight(viewport)
	case outpostStageRestart:
		if !k.started || s.sim.Ticks() != 0 {
			return false, nil
		}
		return true, k.checkSpawn(viewport)
	case outpostStageDragPress:
		return k.started, nil
	case outpostStageDragMove:
		if !k.started {
			return false, nil
		}
		if !s.drag.active || !pixelIs(viewport, 160, 13, selectionColor) {
			return false, errors.New("frame did not show the drag box")
		}
		k.whiteBefore = white(viewport)
		return true, nil
	case outpostStageDragRelease:
		if !k.started {
			return false, nil
		}
		if countPixels(viewport, selectionColor) == 0 || s.drag.active {
			return false, errors.New("the drag box did not turn into a selection")
		}
		return true, k.checkMarkers(viewport, 6)
	case outpostStageHold:
		if !k.started {
			return false, nil
		}
		k.whiteHold = white(viewport)
		if u, _ := s.sim.Unit(1); !u.Hold() || k.whiteHold <= k.whiteBefore {
			return false, errors.New("frame did not show the hold count in the selection text")
		}
		return true, nil
	case outpostStagePause:
		// The pause panel dims the HUD text under it, so only the menu's own text is counted.
		if text := white(
			viewport,
		); app.state != statePaused || s.sim.Ticks() != k.pausedAt ||
			text < 100 {
			return false, fmt.Errorf(
				"frame did not show the pause menu over a stopped match: state %d, tick %d of %d, %d text pixels",
				app.state,
				s.sim.Ticks(),
				k.pausedAt,
				text,
			)
		}
		return true, nil
	case outpostStageSave:
		if _, ok := k.store[outpostSaveKey]; !ok || app.noticeLeft == 0 {
			return false, errors.New("saving did not store the match and show a notice")
		}
		k.savedAt = s.sim.Ticks()
		return true, nil
	case outpostStageResume:
		return app.state == statePlaying, nil
	case outpostStageRun:
		if s.sim.Ticks() != k.savedAt+30 {
			return false, fmt.Errorf("the match ran to tick %d, want %d", s.sim.Ticks(), k.savedAt+30)
		}
		return true, nil
	case outpostStageLoad:
		if app.state != statePlaying || s.sim.Ticks() != k.savedAt {
			return false, nil
		}
		if u, _ := s.sim.Unit(1); !u.Hold() {
			return false, errors.New("the loaded match lost the hold order")
		}
		return true, k.checkMarkers(viewport, 6)
	case outpostStageQuit:
		if _, ok := k.store[outpostReplayKey]; app.state != stateTitle || !ok {
			return false, nil
		}
		return true, nil
	case outpostStageWatch:
		return app.state == stateWatching, nil
	case outpostStageReplay:
		if !app.player.Done() {
			return false, nil
		}
		if app.state != stateWatching || app.player.Played() != app.player.Ticks() ||
			countPixels(viewport, app.recLabel.Style.Text) == 0 {
			return false, fmt.Errorf("the replay ended badly: state %d, notice %q", app.state, app.notice)
		}
		return true, nil
	}
	return false, nil
}

func runOutpostSmoke() error {
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		return err
	}
	smoke := &outpostSmoke{store: smokeStore{}}
	world, config, err := newOutpost(authoredFiles, loaded, smoke.store)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		if smoke.stage >= outpostStageDone || smoke.app == nil {
			return nil
		}
		smoke.waiting = false
		complete, err := smoke.check(viewport)
		if err != nil {
			var tick uint64
			if smoke.app.session != nil {
				tick = smoke.app.session.sim.Ticks()
			}
			return fmt.Errorf("stage %d, tick %d: %w", smoke.stage, tick, err)
		}
		if complete {
			smoke.stage, smoke.started, smoke.pending = smoke.stage+1, false, nil
			if smoke.stage == outpostStageDone {
				cancel()
			}
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, config, func() engine.Scene {
		smoke.app = newOutpostApp(world)
		return &outpostSmokeScene{Scene: smoke.app, smoke: smoke}
	}); err != nil {
		return fmt.Errorf("native outpost smoke: %w", err)
	}
	if smoke.stage != outpostStageDone {
		return fmt.Errorf("native outpost smoke ended during stage %d", smoke.stage)
	}
	return nil
}

package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"strconv"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/engine/save"
	"github.com/krapht-foundation/goje/platform"
	"github.com/krapht-foundation/goje/ui"
)

const (
	outpostHint = "A attack-move  H hold  F focus  Ctrl+1-9 group"
	// Keys in the store: one saved game and the last finished recording.
	outpostSaveKey   = "save"
	outpostReplayKey = "last"
	// outpostNoticeTicks is how long a notice stays on the screen.
	outpostNoticeTicks = 150
)

type outpostState uint8

const (
	stateTitle outpostState = iota
	statePlaying
	statePaused
	stateWatching
)

// Pause menu entries.
const (
	pauseResume = iota
	pauseSave
	pauseLoad
	pauseWatch
	pauseRestart
	pauseQuit
)

// Title menu entries.
const (
	titleStart = iota
	titleLoad
	titleWatch
)

// outpostApp is the whole game: a title menu, a match, a pause menu over it, and
// the replay of the last match. A match runs in its own runtime, stepped only
// while the game is not paused, so a pause is not part of a recording and a
// replay needs no menu. The app reads the live input; the match reads what the
// app passes it.
type outpostApp struct {
	world *outpostWorld
	state outpostState
	menu  ui.Menu

	match    *engine.Runtime // The running match or replay, or nil on the title.
	session  *outpostSession // The scene in match; the factory publishes it.
	recorder *replay.Recorder
	player   *replay.Player

	notice     string
	noticeLeft int
	recLabel   ui.Label
	recSeconds int
	panel      ui.Panel
	err        error
}

func newOutpostApp(world *outpostWorld) *outpostApp {
	style := ui.DefaultStyle()
	return &outpostApp{
		world:    world,
		recLabel: ui.Label{Bounds: image.Rect(240, 0, width, 12), Text: "REC 00:00", Style: style},
		panel:    ui.Panel{Bounds: image.Rect(0, 0, width, height), Style: style},
	}
}

func (a *outpostApp) Enter() error {
	a.recLabel.Style.Text = color.NRGBA{R: 255, G: 96, B: 96, A: 255}
	a.panel.Style.Fill = color.NRGBA{R: 8, G: 10, B: 16, A: 170}
	a.panel.Style.BorderWidth = 0
	a.toTitle()
	return nil
}

func (a *outpostApp) Exit() { a.stopMatch() }

func (a *outpostApp) toTitle() {
	a.stopMatch()
	a.state = stateTitle
	a.menu = ui.Menu{
		Bounds: image.Rect(100, 92, 220, 140),
		Items:  []string{"Start", "Load", "Watch last replay"},
		Style:  ui.DefaultStyle(),
	}
}

func (a *outpostApp) toPause() {
	a.state = statePaused
	a.menu = ui.Menu{
		Bounds: image.Rect(100, 54, 220, 138),
		Items:  []string{"Resume", "Save", "Load", "Watch replay", "Restart", "Quit to title"},
		Style:  ui.DefaultStyle(),
	}
}

func (a *outpostApp) say(text string) { a.notice, a.noticeLeft = text, outpostNoticeTicks }

// sessionFactory creates matches whose scene the app can reach.
func (a *outpostApp) sessionFactory(start []byte) engine.SceneFactory {
	first := start
	return func() engine.Scene {
		s := &outpostSession{world: a.world, restore: first}
		s.board = a.world.board
		first = nil // A restart within the recording begins from the level again.
		a.session = s
		return s
	}
}

// begin starts a recorded match, from a snapshot when start is not nil.
func (a *outpostApp) begin(start []byte) error {
	a.stopMatch()
	recorder, err := replay.NewRecorder(a.world.meta, start, outpostRecordTicks)
	if err != nil {
		return err
	}
	match, err := engine.Start(replay.Record(a.sessionFactory(start), recorder))
	if err != nil {
		return err
	}
	a.match, a.recorder, a.recSeconds = match, recorder, 0
	a.recLabel.Text = "REC 00:00"
	a.state = statePlaying
	return nil
}

// finishRecording ends the recording and keeps it as the last replay. It
// returns the encoded recording, or nil when there is none.
func (a *outpostApp) finishRecording() []byte {
	if a.recorder == nil {
		return nil
	}
	data, err := replay.Encode(a.recorder.Finish())
	a.recorder = nil
	if err != nil {
		a.say("Could not keep the replay: " + err.Error())
		return nil
	}
	if a.world.store != nil {
		if err := a.world.store.Put(outpostReplayKey, data); err != nil {
			a.say("Could not keep the replay: " + err.Error())
		}
	}
	return data
}

func (a *outpostApp) stopMatch() {
	if a.match == nil {
		return
	}
	a.finishRecording()
	if err := a.match.Stop(); err != nil && a.err == nil {
		a.err = err
	}
	a.match, a.session, a.player = nil, nil, nil
}

// load starts a match from the saved game. A problem is a notice, not an error.
func (a *outpostApp) load() {
	if a.world.store == nil {
		a.say(errOutpostSaving.Error())
		return
	}
	data, err := a.world.store.Get(outpostSaveKey)
	if errors.Is(err, platform.ErrNotFound) {
		a.say("There is no saved game.")
		return
	}
	if err != nil {
		a.say("Could not load: " + err.Error())
		return
	}
	meta, snapshot, err := save.Decode(data)
	if err == nil {
		err = meta.Check(a.world.meta)
	}
	if err != nil {
		a.say("Could not load: " + err.Error())
		return
	}
	if err := a.begin(snapshot); err != nil {
		a.say("Could not load: " + err.Error())
		a.toTitle()
	}
}

func (a *outpostApp) save() {
	if a.world.store == nil {
		a.say(errOutpostSaving.Error())
		return
	}
	snapshot, err := a.session.Snapshot()
	if err == nil {
		var data []byte
		if data, err = save.Encode(a.world.meta, snapshot); err == nil {
			err = a.world.store.Put(outpostSaveKey, data)
		}
	}
	if err != nil {
		a.say("Could not save: " + err.Error())
		return
	}
	a.say("Game saved.")
}

// watch plays a recording in a new runtime. It reports a problem as a notice.
func (a *outpostApp) watch(data []byte) {
	if data == nil {
		a.say("There is no replay yet.")
		return
	}
	recording, err := replay.Decode(data)
	var player *replay.Player
	if err == nil {
		player, err = replay.Play(a.sessionFactory(nil), recording, a.world.meta)
	}
	var match *engine.Runtime
	if err == nil {
		a.stopMatch()
		match, err = engine.Start(player.Factory())
	}
	if err != nil {
		a.say("Could not play the replay: " + err.Error())
		return
	}
	a.match, a.player, a.state = match, player, stateWatching
}

func (a *outpostApp) lastReplay() []byte {
	if a.world.store == nil {
		return nil
	}
	data, err := a.world.store.Get(outpostReplayKey)
	if err != nil {
		return nil
	}
	return data
}

func (a *outpostApp) Update(tick engine.Tick) (engine.Transition, error) {
	if a.err != nil {
		return engine.Transition{}, a.err
	}
	if a.noticeLeft > 0 {
		a.noticeLeft--
	}
	in := tick.Input
	var err error
	switch a.state {
	case stateTitle:
		err = a.updateTitle(in)
	case statePlaying:
		err = a.updatePlaying(in)
	case statePaused:
		err = a.updatePaused(in)
	case stateWatching:
		err = a.updateWatching(in)
	}
	return engine.Transition{}, err
}

func (a *outpostApp) updateTitle(in engine.Input) error {
	choice, _ := a.menu.Update(in)
	switch choice {
	case titleStart:
		return a.begin(nil)
	case titleLoad:
		a.load()
	case titleWatch:
		a.watch(a.lastReplay())
	}
	return nil
}

func (a *outpostApp) updatePlaying(in engine.Input) error {
	if in.Pressed.Has(engine.KeyEscape) && !a.session.cancelable() {
		a.toPause()
		return nil
	}
	if err := a.match.Step(in); err != nil {
		return err
	}
	if seconds := a.recorder.Ticks() / 60; seconds != a.recSeconds {
		a.recSeconds = seconds
		a.recLabel.Text = "REC " + twoDigits(seconds/60) + ":" + twoDigits(seconds%60)
	}
	return nil
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func (a *outpostApp) updatePaused(in engine.Input) error {
	choice, cancelled := a.menu.Update(in)
	if cancelled {
		choice = pauseResume
	}
	switch choice {
	case pauseResume:
		a.state = statePlaying
	case pauseSave:
		a.save()
	case pauseLoad:
		a.load()
	case pauseWatch:
		// Watching ends this match, so a replay that cannot play returns to the title.
		a.watch(a.finishRecording())
		if a.state != stateWatching {
			a.toTitle()
		}
	case pauseRestart:
		// The restart is an input like any other, so the recording includes it.
		if err := a.match.Step(engine.Input{Pressed: engine.KeyR}); err != nil {
			return err
		}
		a.state = statePlaying
	case pauseQuit:
		a.toTitle()
	}
	return nil
}

func (a *outpostApp) updateWatching(in engine.Input) error {
	if in.Pressed.Has(engine.KeyEscape) {
		a.toTitle()
		return nil
	}
	if a.player.Done() {
		return nil
	}
	if err := a.match.Step(engine.Input{}); err != nil {
		if divergence, ok := errors.AsType[*replay.DivergenceError](err); ok {
			a.say(divergence.Error())
			a.toTitle()
			return nil
		}
		return err
	}
	return nil
}

func (a *outpostApp) Draw(canvas engine.Canvas) {
	face := a.world.face
	if a.match != nil {
		if err := a.match.Draw(canvas); err != nil && a.err == nil {
			a.err = fmt.Errorf("draw match: %w", err)
		}
	} else {
		canvas.Clear(color.NRGBA{R: 14, G: 18, B: 28, A: 255})
	}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	switch a.state {
	case stateTitle:
		face.Draw(canvas, "OUTPOST", 97, 36, 3, white)
		face.Draw(canvas, "Commands, replays, and saves", 76, 66, 1, skirmishAlarmQuiet)
		a.menu.Draw(canvas, face)
		face.Draw(canvas, "Arrows and Enter, or the mouse", 74, 150, 1, skirmishAlarmQuiet)
	case statePlaying:
		a.recLabel.Draw(canvas, face)
	case statePaused:
		a.panel.Draw(canvas)
		face.Draw(canvas, "PAUSED", 128, 38, 2, white)
		a.menu.Draw(canvas, face)
	case stateWatching:
		label := ui.Label{
			Bounds: a.recLabel.Bounds, Style: a.recLabel.Style,
			Text: "REPLAY " + strconv.Itoa(a.player.Played()/60) + "s",
		}
		if a.player.Done() {
			label.Text = "REPLAY ENDED"
		}
		label.Draw(canvas, face)
		face.Draw(canvas, "Esc leaves the replay", 100, 169, 1, skirmishAlarmQuiet)
	}
	if a.noticeLeft > 0 {
		face.Draw(canvas, a.notice, 8, 160, 1, color.NRGBA{R: 255, G: 235, B: 87, A: 255})
	}
}

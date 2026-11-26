package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"strconv"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/engine/save"
	"github.com/krapht-foundation/goje/netlink"
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
	stateLobby       // Connecting to the relay and waiting for the other player.
	stateOnline      // An online match, which pauses only during recovery.
	stateOnlineEnded // The reason an online match or lobby ended.
	stateMatchWatch  // Watching an online command log without a connection.
	stateLiveWatch   // Watching a live match without taking a player slot.
)

// Pause menu entries.
const (
	pauseResume = iota
	pauseSave
	pauseLoad
	pauseWatch
	pauseRestart
	pauseSound
	pauseQuit
)

// Title menu entries.
const (
	titleStart = iota
	titleLoad
	titleWatch
	titleGuards
	titleWatchMatch
	titleHostCoop
	titleHostVersus
	titleJoin
)

// Online menu entries, shown over a match that keeps running.
const (
	onlineResume = iota
	onlineSound
	onlineLeave
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

	match       *engine.Runtime // The running match or replay, or nil on the title.
	session     *outpostSession // The scene in match; the factory publishes it.
	recorder    *replay.Recorder
	player      *replay.Player
	matchPlayer *matchWatch
	lastMatch   []byte
	tactics     bool // The title's guard option, not the option of a loaded match.

	notice     string
	noticeLeft int
	recLabel   ui.Label
	recSeconds int
	panel      ui.Panel
	err        error

	sound     engine.SoundOutput // This tick's output, or nil. Valid only inside Update.
	musicOn   bool
	musicGain float32

	// start is true on the tick a gamepad's Start button goes down, and menuButton
	// is the touch button that opens the pause menu or leaves a replay.
	start, prevStart bool
	menuButton       ui.Button
	fogButton        ui.Button

	lobby      *onlineLobby // The connection being made, or nil.
	osync      *onlineSync  // The gate of the online match, or nil.
	conn       *netlink.Conn
	live       *liveWatch
	view       viewSender
	viewLabel  string
	recovery   *outpostRecovery
	onlineMenu bool     // The menu is open over an online match.
	endLines   []string // The lines of the end screen.
}

func newOutpostApp(world *outpostWorld) *outpostApp {
	style := ui.DefaultStyle()
	return &outpostApp{
		world:      world,
		recLabel:   ui.Label{Bounds: image.Rect(240, 0, width, 12), Text: "REC 00:00", Style: style},
		panel:      ui.Panel{Bounds: image.Rect(0, 0, width, height), Style: style},
		menuButton: ui.Button{Bounds: image.Rect(276, 14, 318, 26), Style: style},
		fogButton:  ui.Button{Bounds: image.Rect(224, 14, 273, 26), Text: "Fog", Style: style},
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
	items := []string{
		"Start",
		"Load",
		"Watch last replay",
		a.guardsLabel(),
		"Watch last online match",
	}
	top, row := 92, 16
	if online := a.world.online; online.relay != "" {
		top, row = 76, 12
		items = append(items, "Host co-op", "Host versus")
		if online.room != "" {
			action := "Join room "
			if online.watch {
				action = "Watch room "
			}
			items = append(items, action+online.room)
		}
	}
	a.menu = ui.Menu{
		Bounds: image.Rect(72, top, 248, top+row*len(items)),
		Items:  items,
		Style:  ui.DefaultStyle(),
	}
}

func (a *outpostApp) guardsLabel() string {
	if a.tactics {
		return "Guards: Tactical"
	}
	return "Guards: Classic"
}

func (a *outpostApp) toPause() {
	a.state = statePaused
	a.menu = ui.Menu{
		Bounds: image.Rect(100, 54, 220, 152),
		Items: []string{
			"Resume", "Save", "Load", "Watch replay", "Restart", a.soundLabel(), "Quit to title",
		},
		Style: ui.DefaultStyle(),
	}
}

// soundLabel is the pause menu entry that shows the volume.
func (a *outpostApp) soundLabel() string {
	switch a.world.volume {
	case 0:
		return "Sound: off"
	case 1:
		return "Sound: full"
	default:
		return "Sound: half"
	}
}

// cycleVolume steps the volume from full to half to off and back. It is a
// setting of the player, so it is neither recorded nor part of a match.
func (a *outpostApp) cycleVolume() {
	switch a.world.volume {
	case 1:
		a.world.volume = 0.5
	case 0.5:
		a.world.volume = 0
	default:
		a.world.volume = 1
	}
}

// click plays a menu sound.
func (a *outpostApp) click() {
	if a.sound != nil && a.world.volume > 0 {
		a.sound.Play(cueClick, a.world.volume, 0)
	}
}

// closeOnline leaves an online match and closes its connection and lobby.
// A viewer leaving does not end the room.
func (a *outpostApp) closeOnline() {
	if a.lobby != nil {
		a.lobby.close()
		a.lobby = nil
	}
	if a.recovery != nil {
		a.recovery.close()
		a.recovery = nil
	}
	if a.osync != nil {
		a.finishMatchLog()
		if a.conn == nil {
			noteClose("leave the match", a.osync.lock.Leave())
		} else {
			noteClose("end the room", a.conn.EndRoom())
		}
		a.osync = nil
	}
	if a.conn != nil {
		noteClose("close the connection", a.conn.Close())
		a.conn = nil
	}
	a.live, a.view, a.viewLabel = nil, viewSender{}, ""
	a.onlineMenu = false
}

func (a *outpostApp) say(text string) { a.notice, a.noticeLeft = text, outpostNoticeTicks }

// sessionFactory creates matches whose scene the app can reach.
func (a *outpostApp) sessionFactory(start []byte, tactics bool) engine.SceneFactory {
	first := start
	return func() engine.Scene {
		s := a.world.newSession(first)
		s.tactics = tactics
		first = nil // A restart within the recording begins from the level again.
		a.session = s
		return s
	}
}

// begin starts a recorded match, from a snapshot when start is not nil.
func (a *outpostApp) begin(start []byte, tactics bool) error {
	a.stopMatch()
	recorder, err := replay.NewRecorder(a.world.soloMeta(tactics), start, outpostRecordTicks)
	if err != nil {
		return err
	}
	match, err := engine.Start(replay.Record(a.sessionFactory(start, tactics), recorder))
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
	a.closeOnline()
	if a.match == nil {
		return
	}
	a.finishRecording()
	if err := a.match.Stop(); err != nil && a.err == nil {
		a.err = err
	}
	a.match, a.session, a.player, a.matchPlayer = nil, nil, nil, nil
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
	tactics := meta == a.world.soloMeta(true)
	if err == nil {
		err = meta.Check(a.world.soloMeta(tactics))
	}
	if err != nil {
		a.say("Could not load: " + err.Error())
		return
	}
	if err := a.begin(snapshot, tactics); err != nil {
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
		if data, err = save.Encode(a.world.soloMeta(a.session.tactics), snapshot); err == nil {
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
		tactics := recording.Meta == a.world.soloMeta(true)
		player, err = replay.Play(a.sessionFactory(nil, tactics), recording, a.world.soloMeta(tactics))
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
	pad, _ := firstPad(in)
	padStart := pad.Buttons.Has(engine.GamepadStart)
	a.start, a.prevStart = padStart && !a.prevStart, padStart
	a.sound = tick.Sound()
	if a.match != nil {
		a.match.SetSoundOutput(a.sound)
	}
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
	case stateMatchWatch:
		a.updateMatchWatch(in)
	case stateLiveWatch:
		a.updateLiveWatch(in)
	case stateLobby:
		a.updateLobby(in)
	case stateOnline:
		err = a.updateOnline(in)
	case stateOnlineEnded:
		a.updateOnlineEnded(in)
	}
	a.updateMusic(tick)
	return engine.Transition{}, err
}

func (a *outpostApp) updateTitle(in engine.Input) error {
	choice, _ := a.menu.Update(in)
	if choice >= 0 {
		a.click()
	}
	switch choice {
	case titleStart:
		return a.begin(nil, a.tactics)
	case titleLoad:
		a.load()
	case titleWatch:
		a.watch(a.lastReplay())
	case titleGuards:
		a.tactics = !a.tactics
		a.menu.Items[titleGuards] = a.guardsLabel()
	case titleWatchMatch:
		a.watchMatch()
	case titleHostCoop, titleHostVersus:
		a.stopMatch()
		a.lobby, a.state = startLobby(a.world.online, true, false), stateLobby
		a.lobby.setup.tactics = a.tactics
		if choice == titleHostVersus {
			a.lobby.setup = matchSetup{mode: matchlog.Versus}
		}
	case titleJoin:
		a.stopMatch()
		a.lobby, a.state = startLobby(a.world.online, false, a.world.online.watch), stateLobby
	}
	return nil
}

func (a *outpostApp) updatePlaying(in engine.Input) error {
	touchMenu := a.session.controls.touchUI && a.menuButton.Update(in)
	if a.start || touchMenu || (in.Pressed.Has(engine.KeyEscape) && !a.session.cancelable()) {
		a.click()
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
	if cancelled || a.start {
		choice = pauseResume
	}
	if choice >= 0 {
		a.click()
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
	case pauseSound:
		a.cycleVolume()
		a.menu.Items[pauseSound] = a.soundLabel()
	case pauseQuit:
		a.toTitle()
	}
	return nil
}

func (a *outpostApp) updateWatching(in engine.Input) error {
	if in.Pressed.Has(engine.KeyEscape) || a.start || a.menuButton.Update(in) {
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
		face.Draw(
			canvas,
			"Arrows and Enter, a gamepad, or touch",
			49,
			float32(a.menu.Bounds.Max.Y+10),
			1,
			skirmishAlarmQuiet,
		)
	case stateLobby:
		a.drawLobby(canvas, face)
	case stateOnline:
		a.drawOnline(canvas, face)
	case stateOnlineEnded:
		a.drawOnlineEnded(canvas, face)
	case statePlaying:
		a.recLabel.Draw(canvas, face)
		if a.session.controls.touchUI {
			a.menuButton.Text = "Menu"
			a.menuButton.Draw(canvas, face)
		}
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
		a.menuButton.Text = "Leave"
		a.menuButton.Draw(canvas, face)
		face.Draw(canvas, "Esc leaves the replay", 100, 169, 1, skirmishAlarmQuiet)
	case stateMatchWatch:
		a.drawMatchWatch(canvas)
	case stateLiveWatch:
		a.drawLiveWatch(canvas, face)
	}
	if a.noticeLeft > 0 {
		face.Draw(canvas, a.notice, 8, 160, 1, color.NRGBA{R: 255, G: 235, B: 87, A: 255})
	}
}

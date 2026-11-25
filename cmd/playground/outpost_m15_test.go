package main

import (
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/examples/skirmish"
)

// voice is one sound request, with the tick of the step that made it.
type voice struct {
	step      int
	cue       int
	gain, pan float32
}

// voiceLog is a SoundOutput that keeps what it is asked to play.
type voiceLog struct {
	step   int
	voices []voice
}

func (l *voiceLog) Play(cue int, gain, pan float32) {
	l.voices = append(l.voices, voice{l.step, cue, gain, pan})
}
func (*voiceLog) Clear() {}

// runWithSound plays the inputs through the factory and returns every voice.
func runWithSound(tb testing.TB, factory engine.SceneFactory, inputs []engine.Input) []voice {
	tb.Helper()
	runtime, err := engine.Start(factory)
	if err != nil {
		tb.Fatal(err)
	}
	log := &voiceLog{}
	runtime.SetSoundOutput(log)
	for i, in := range inputs {
		log.step = i
		if err := runtime.Step(in); err != nil {
			tb.Fatal(err)
		}
	}
	if err := runtime.Stop(); err != nil {
		tb.Fatal(err)
	}
	return log.voices
}

func TestOutpostFogHidesGuardsUntilTheScoutsSeeThem(t *testing.T) {
	s := newOutpostSession(t)
	guards := func() int {
		n := 0
		for _, item := range s.items {
			if u, ok := s.sim.Unit(item.ID); ok && u.Team == skirmish.Guards {
				n++
			}
		}
		return n
	}
	if got := guards(); got != 0 || s.hud.foes != 0 {
		t.Fatalf("%d guards are drawn and %d counted at the start, want none", got, s.hud.foes)
	}
	w, h := s.board.Size()
	explored := 0
	for _, cell := range s.groundCells {
		if s.sim.Explored(cell) {
			explored++
		}
	}
	if explored == 0 || explored >= len(s.ground) || w*h == 0 {
		t.Fatalf(
			"%d of %d walkable cells are explored at the start, want some but not all",
			explored,
			len(s.ground),
		)
	}
	seen := false
	for _, in := range outpostScript(t) {
		s.update(t, in)
		seen = seen || guards() > 0
		for _, item := range s.items {
			if u, ok := s.sim.Unit(item.ID); ok && u.Team == skirmish.Guards && !s.seenUnit(u) {
				t.Fatalf(
					"a guard the scouts cannot see is in the presentation list at tick %d",
					s.sim.Ticks(),
				)
			}
		}
	}
	if !seen {
		t.Error("the scouts never saw a guard")
	}
}

func TestOutpostSoundsComeFromTheMatchAndAreRateLimited(t *testing.T) {
	world := newOutpostWorld(t)
	inputs := outpostScript(t)
	factory := func() engine.Scene { return world.newSession(nil) }
	voices := runWithSound(t, factory, inputs)
	heard := map[int]int{}
	last := map[int]int{}
	for _, v := range voices {
		heard[v.cue]++
		if v.gain <= 0 || v.gain > 1 || v.pan < -1 || v.pan > 1 {
			t.Fatalf("voice %+v has a gain or pan out of range", v)
		}
		if at, ok := last[v.cue]; ok && v.step-at < outpostCueGap {
			t.Fatalf(
				"cue %d played at steps %d and %d, closer than %d ticks",
				v.cue,
				at,
				v.step,
				outpostCueGap,
			)
		}
		last[v.cue] = v.step
	}
	for _, cue := range []int{cueBolt, cueHit, cueFall, cueOrder, cueVictory} {
		if heard[cue] == 0 {
			t.Errorf("the winning match never played cue %d (heard %v)", cue, heard)
		}
	}
	if heard[cueDefeat] != 0 {
		t.Errorf("a won match played the defeat sound")
	}

	// Sound is output: the same match sounds the same, and a muted match plays out identically.
	if again := runWithSound(t, factory, inputs); !slices.Equal(voices, again) {
		t.Error("the same inputs made different sounds")
	}
	muted := newOutpostWorld(t)
	muted.volume = 0
	var loud, quiet *outpostSession
	runWithSound(t, func() engine.Scene { loud = world.newSession(nil); return loud }, inputs)
	if got := runWithSound(
		t,
		func() engine.Scene { quiet = muted.newSession(nil); return quiet },
		inputs,
	); len(
		got,
	) != 0 {
		t.Errorf("a muted match made %d sounds", len(got))
	}
	if loud.sim.Trace() != quiet.sim.Trace() || loud.StateHash() != quiet.StateHash() {
		t.Error("muting changed the match")
	}
}

// pad returns an input with one connected pad holding the buttons.
func pad(buttons engine.GamepadButtons, leftX int8) engine.Input {
	var in engine.Input
	in.Gamepads[0] = engine.Gamepad{Connected: true, Buttons: buttons, LeftX: leftX}
	return in
}

func TestOutpostGamepadMovesACursorAndClicks(t *testing.T) {
	s := newOutpostSession(t)
	s.update(t, pad(0, 127))
	if !s.controls.padActive || s.controls.cursorX != width/2+padCursorSpeed {
		t.Fatalf(
			"the cursor is at %v after a full push, active %v",
			s.controls.cursorX,
			s.controls.padActive,
		)
	}
	// Click the first scout with the south button, releasing on the next tick.
	u, _ := s.sim.Unit(1)
	c := u.Center()
	s.controls.cursorX, s.controls.cursorY = s.logical(
		c.X.Float64(),
		c.Y.Float64(),
		skirmishUnitHeight/2,
	)
	s.update(t, pad(engine.GamepadSouth, 0))
	s.update(t, pad(0, 0))
	if len(s.selection) != 1 {
		t.Fatalf("a pad click selected %d scouts, want 1", len(s.selection))
	}
	// The west button arms an attack-move and the east button gives a move order.
	s.update(t, pad(engine.GamepadWest, 0))
	if s.mode != modeAttackMove {
		t.Fatalf("mode = %d after the west button, want an attack-move", s.mode)
	}
	s.update(t, pad(0, 0))
	s.update(t, pad(engine.GamepadEast, 0))
	if s.mode != modeNone {
		t.Errorf("the east button left mode %d", s.mode)
	}
	s.update(t, pad(0, 0))
	// A mouse move hands control back to the mouse.
	mouse := engine.Input{Pointer: engine.Pointer{X: 10, Y: 10, Inside: true}}
	mouse.Gamepads[0] = engine.Gamepad{Connected: true}
	s.update(t, mouse)
	if s.controls.padActive {
		t.Error("the pad cursor stayed active after the mouse moved")
	}
}

func TestOutpostTouchButtonsGiveCommandsWithoutStartingADrag(t *testing.T) {
	s := newOutpostSession(t)
	s.selectAll(t)
	tap := func(x, y float64) {
		press, release := drag(x, y, x, y, 0)
		press.Touches[0] = engine.Touch{ID: 1, X: x, Y: y, Active: true}
		s.update(t, press)
		s.update(t, release)
	}
	tap(20, 20) // Any touch shows the buttons; this one hits the empty ground.
	if !s.controls.touchUI {
		t.Fatal("a touch did not show the command buttons")
	}
	if len(s.selection) != 0 {
		t.Fatalf("a tap on the empty ground left %d scouts selected", len(s.selection))
	}
	s.selectAll(t)
	selected := len(s.selection)
	tap(
		float64(s.controls.buttons[touchMove].Bounds.Min.X+4),
		float64(s.controls.buttons[touchMove].Bounds.Min.Y+4),
	)
	if s.mode != modeMove || len(s.selection) != selected {
		t.Fatalf(
			"the Move button left mode %d and %d scouts selected, want a move mode and %d",
			s.mode,
			len(s.selection),
			selected,
		)
	}
	gx, gy := s.logical(3.5, 4.5, 0)
	tap(gx, gy)
	if s.mode != modeNone {
		t.Errorf("the tap after Move left mode %d", s.mode)
	}
	if _, moving := s.sim.Goal(mustUnit(t, s, s.selection[0])); !moving {
		t.Error("the scouts were not sent anywhere")
	}
	// A mouse press hides the buttons again.
	press, release := drag(5, 5, 5, 5, 0)
	s.update(t, press)
	s.update(t, release)
	if s.controls.touchUI {
		t.Error("a mouse press did not hide the touch buttons")
	}
}

func mustUnit(tb testing.TB, s *outpostSession, id uint64) *skirmish.Unit {
	tb.Helper()
	u, ok := s.sim.Unit(id)
	if !ok {
		tb.Fatalf("no unit %d", id)
	}
	return u
}

// padScript plays a match from a pad alone: the cursor drives a box over the
// scouts, then they are sent to the middle.
func padScript(tb testing.TB) []engine.Input {
	tb.Helper()
	var inputs []engine.Input
	step := func(in engine.Input) { inputs = append(inputs, in) }
	step(pad(0, 0)) // Wake the pad.
	for range 40 {  // Left and up to the top left corner.
		in := pad(0, -127)
		in.Gamepads[0].LeftY = -127
		step(in)
	}
	step(pad(engine.GamepadSouth, 0))
	for range 120 { // Drag the box to the bottom right.
		in := pad(engine.GamepadSouth, 127)
		in.Gamepads[0].LeftY = 127
		step(in)
	}
	step(pad(0, 0))
	for range 30 {
		step(pad(engine.GamepadRightShoulder, 0))
		step(pad(0, 0))
	}
	return inputs
}

func TestOutpostReplaysAMatchPlayedOnAGamepad(t *testing.T) {
	world := newOutpostWorld(t)
	inputs := padScript(t)
	var last *outpostSession
	factory := func() engine.Scene { last = world.newSession(nil); return last }
	recorder, err := replay.NewRecorder(world.meta, nil, len(inputs))
	if err != nil {
		t.Fatal(err)
	}
	runOutpost(t, replay.Record(factory, recorder), inputs)
	if len(last.selection) == 0 {
		t.Fatal("the pad script selected nothing")
	}
	data, err := replay.Encode(recorder.Finish())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = replay.Verify(factory, decoded, world.meta); err != nil {
		t.Fatalf("the pad match does not replay: %v", err)
	}

	snapshot, err := last.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := newOutpostSession(t)
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if restored.StateHash() != last.StateHash() || restored.controls != last.controls {
		t.Error("a restored match differs from the one that was saved")
	}
}

func TestOutpostGamepadStartOpensAndClosesThePauseMenu(t *testing.T) {
	h := newAppHarness(t)
	h.step(engine.KeyEnter)
	h.run(2)
	if h.app.state != statePlaying {
		t.Fatalf("state = %d after Start, want playing", h.app.state)
	}
	press := func(buttons engine.GamepadButtons) {
		if err := h.runtime.Step(pad(buttons, 0)); err != nil {
			t.Fatal(err)
		}
	}
	press(engine.GamepadStart)
	press(0)
	if h.app.state != statePaused {
		t.Fatalf("state = %d after the Start button, want paused", h.app.state)
	}
	press(engine.GamepadStart)
	press(0)
	if h.app.state != statePlaying {
		t.Fatalf("state = %d after the Start button again, want playing", h.app.state)
	}
}

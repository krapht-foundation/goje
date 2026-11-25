package main

import (
	"encoding/binary"
	"image"
	"math"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/ui"
)

const (
	// padCursorSpeed is how far the gamepad cursor moves in one tick at full
	// stick deflection, in logical pixels.
	padCursorSpeed = 3.0
	// padPanThreshold is how far the right stick must be pushed, out of 127, before
	// it pans the camera like an arrow key.
	padPanThreshold = 40
)

// outpostControls is the state of the touch buttons and the gamepad cursor.
// It is part of the match, so it is hashed and saved: it decides which taps
// become commands and where a pad click lands.
type outpostControls struct {
	cursorX, cursorY float64
	lastPointer      engine.Pointer
	prevPad          engine.GamepadButtons
	padActive        bool
	touchUI          bool
	padGroup         int
	buttons          [4]ui.Button
}

// The touch buttons, in the order they are laid out along the bottom of the board.
const (
	touchMove = iota
	touchAttackMove
	touchHold
	touchFocus
)

var touchLabels = [len(outpostControls{}.buttons)]string{
	touchMove: "Move", touchAttackMove: "Attack", touchHold: "Hold", touchFocus: "Focus",
}

func (c *outpostControls) layout() {
	style := ui.DefaultStyle()
	const barTop, barHeight = 156, 22
	for i := range c.buttons {
		left := i * width / len(c.buttons)
		right := (i + 1) * width / len(c.buttons)
		c.buttons[i] = ui.Button{
			Bounds: image.Rect(left+1, barTop, right-1, barTop+barHeight),
			Text:   touchLabels[i],
			Style:  style,
		}
	}
	c.cursorX, c.cursorY = width/2, height/2
}

// effective turns the raw input into what the match reads. A pad with a
// connected standard layout drives a cursor that presses like the mouse, the
// face buttons and shoulders stand in for keys, and the right stick pans the
// camera. Touch presses on the command buttons are consumed and run a command.
// A real mouse move or press hands control back to the mouse.
func (s *outpostSession) effective(in engine.Input) engine.Input {
	c := &s.controls
	in = s.touchButtons(in)
	pad, ok := firstPad(in)
	if !ok {
		c.padActive, c.prevPad, c.lastPointer = false, 0, in.Pointer
		return in
	}
	if in.Pointer != c.lastPointer {
		c.padActive = false
		if in.Pointer.Inside {
			c.cursorX, c.cursorY = in.Pointer.X, in.Pointer.Y
		}
	}
	c.lastPointer = in.Pointer
	if pad.LeftX != 0 || pad.LeftY != 0 || pad.Buttons != c.prevPad {
		c.padActive = true
	}
	if !c.padActive {
		c.prevPad = pad.Buttons
		return in
	}
	c.cursorX = min(max(c.cursorX+float64(pad.LeftX)/127*padCursorSpeed, 0), width-1)
	c.cursorY = min(max(c.cursorY+float64(pad.LeftY)/127*padCursorSpeed, 0), height-1)
	at := engine.MousePress{X: c.cursorX, Y: c.cursorY, Valid: true}
	in.Pointer = engine.Pointer{X: c.cursorX, Y: c.cursorY, Inside: true}
	pressed, released := pad.Buttons&^c.prevPad, c.prevPad&^pad.Buttons
	in.PrimaryHeld = pad.Buttons.Has(engine.GamepadSouth)
	if pressed.Has(engine.GamepadSouth) {
		in.PrimaryPress = at
	}
	if released.Has(engine.GamepadSouth) {
		in.PrimaryRelease = at
	}
	if pressed.Has(engine.GamepadEast) {
		in.SecondaryPress = at
	}
	if pressed.Has(engine.GamepadWest) {
		in.Pressed |= engine.KeyA
	}
	if pressed.Has(engine.GamepadNorth) {
		in.Pressed |= engine.KeyH
	}
	if pressed.Has(engine.GamepadLeftTrigger) {
		in.Pressed |= engine.KeyS
	}
	if pressed.Has(engine.GamepadRightTrigger) {
		in.Pressed |= engine.KeyW
	}
	if pressed.Has(engine.GamepadRightShoulder) {
		in.Pressed |= s.cycleGroup(1)
	}
	if pressed.Has(engine.GamepadLeftShoulder) {
		in.Pressed |= s.cycleGroup(-1)
	}
	in.Held |= padPan(pad)
	c.prevPad = pad.Buttons
	return in
}

// padPan maps the right stick to the arrow keys that pan the camera.
func padPan(pad engine.Gamepad) (held engine.Keys) {
	switch {
	case pad.RightX >= padPanThreshold:
		held |= engine.KeyArrowRight
	case pad.RightX <= -padPanThreshold:
		held |= engine.KeyArrowLeft
	}
	switch {
	case pad.RightY >= padPanThreshold:
		held |= engine.KeyArrowDown
	case pad.RightY <= -padPanThreshold:
		held |= engine.KeyArrowUp
	}
	return held
}

// firstPad returns the first connected pad.
func firstPad(in engine.Input) (engine.Gamepad, bool) {
	for _, pad := range in.Gamepads {
		if pad.Connected {
			return pad, true
		}
	}
	return engine.Gamepad{}, false
}

// cycleGroup returns the number key of the next control group in the given
// direction that has scouts, as if the player had pressed it, or 0 if none has.
func (s *outpostSession) cycleGroup(step int) engine.Keys {
	c := &s.controls
	for i := 1; i <= outpostGroups; i++ {
		g := ((c.padGroup+step*i)%outpostGroups + outpostGroups) % outpostGroups
		if len(s.groups[g]) > 0 {
			c.padGroup = g
			return engine.Key1 << g
		}
	}
	return 0
}

// touchButtons shows the command buttons once a finger has pressed, hides them when
// a mouse button does, and consumes a press, a release, and the hold of a press that
// lands on a button, so that it never starts a drag.
func (s *outpostSession) touchButtons(in engine.Input) engine.Input {
	c := &s.controls
	switch {
	case in.PrimaryPress.Valid && in.Touches[0].Active:
		c.touchUI = true
	case in.PrimaryPress.Valid || in.SecondaryPress.Valid:
		c.touchUI = false
	}
	if !c.touchUI {
		return in
	}
	ran := false
	for i := range c.buttons {
		clicked := c.buttons[i].Update(in)
		ran = ran || clicked || c.buttons[i].Down
		if clicked {
			s.runTouchCommand(i)
		}
	}
	if ran {
		in.PrimaryPress, in.PrimaryRelease, in.PrimaryHeld = engine.MousePress{}, engine.MousePress{}, false
	}
	return in
}

// runTouchCommand does what the right mouse button and the keys do.
func (s *outpostSession) runTouchCommand(button int) {
	switch button {
	case touchMove:
		if len(s.selection) > 0 {
			s.mode = modeMove
		}
	case touchAttackMove:
		if len(s.selection) > 0 {
			s.mode = modeAttackMove
		}
	case touchHold:
		s.toggleHold()
	case touchFocus:
		s.focus(engine.Pointer{})
	}
}

func (c *outpostControls) draw(canvas engine.Canvas, face font.Face) {
	if c.touchUI {
		for i := range c.buttons {
			c.buttons[i].Draw(canvas, face)
		}
	}
	if c.padActive {
		x, y := float32(c.cursorX), float32(c.cursorY)
		canvas.Rect(x-4, y, 9, 1, selectionColor)
		canvas.Rect(x, y-4, 1, 9, selectionColor)
	}
}

// hash feeds the state that decides how later input is read.
func (c *outpostControls) hash(add func(uint64)) {
	add(math.Float64bits(c.cursorX))
	add(math.Float64bits(c.cursorY))
	add(math.Float64bits(c.lastPointer.X))
	add(math.Float64bits(c.lastPointer.Y))
	add(uint64(c.prevPad))
	add(uint64(boolByte(c.lastPointer.Inside)) | uint64(boolByte(c.padActive))<<1 |
		uint64(
			boolByte(c.touchUI),
		)<<2 | uint64(c.padGroup)<<8) // #nosec G115 -- padGroup is below outpostGroups.
}

// appendSnapshot writes the controls after the rest of the match snapshot.
func (c *outpostControls) appendSnapshot(b []byte) []byte {
	for _, v := range [...]float64{c.cursorX, c.cursorY, c.lastPointer.X, c.lastPointer.Y} {
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
	}
	b = binary.LittleEndian.AppendUint16(b, uint16(c.prevPad))
	return append(b, boolByte(c.lastPointer.Inside), boolByte(c.padActive), boolByte(c.touchUI),
		byte(c.padGroup)) // #nosec G115 -- padGroup is below outpostGroups.
}

// readControls parses what appendSnapshot wrote. The caller checks r.err and
// calls valid before it applies the result.
func readControls(r *snapshotReader) (c outpostControls) {
	c.cursorX, c.cursorY = math.Float64frombits(r.u64()), math.Float64frombits(r.u64())
	c.lastPointer.X, c.lastPointer.Y = math.Float64frombits(r.u64()), math.Float64frombits(r.u64())
	c.prevPad = engine.GamepadButtons(r.u16())
	c.lastPointer.Inside, c.padActive, c.touchUI = r.byte() == 1, r.byte() == 1, r.byte() == 1
	c.padGroup = int(r.byte())
	return c
}

// valid reports whether restored controls are finite, inside the viewport, and in range.
func (c *outpostControls) valid() bool {
	return finiteWithin(c.cursorX, width/2, width/2) && finiteWithin(c.cursorY, height/2, height/2) &&
		!math.IsNaN(c.lastPointer.X) && !math.IsInf(c.lastPointer.X, 0) &&
		!math.IsNaN(c.lastPointer.Y) && !math.IsInf(c.lastPointer.Y, 0) &&
		c.padGroup < outpostGroups
}

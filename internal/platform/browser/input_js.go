//go:build wasm

package browser

import (
	"syscall/js"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/loop"
)

// touchEvent applies one Pointer Event whose pointerType is "touch". The
// caller has already called preventDefault, which together with the canvas's
// touch-action of none keeps the page from scrolling or zooming and stops the
// compatibility mouse events that would otherwise repeat the touch. Fingers
// that land outside the viewport, including its letterbox bars, are ignored,
// as mouse presses there are.
func (r *renderer) touchEvent(kind string, window, canvas, e js.Value, l *loop.Loop) {
	id := e.Get("pointerId").Int()
	switch kind {
	case "pointerdown":
		if x, y, ok := r.logicalPointer(window, canvas, e); ok {
			l.TouchDown(id, x, y)
		}
	case "pointermove":
		x, y, ok := r.logicalPointer(window, canvas, e)
		l.TouchMove(id, x, y, ok)
	case "pointerup":
		l.TouchUp(id)
	case "pointercancel":
		l.TouchCancel(id)
	}
}

// gamepadReader polls the Gamepad API once per animation frame. Pads whose
// mapping is not "standard" are reported as disconnected.
type gamepadReader struct {
	navigator js.Value
	standard  js.Value
	// available is false when the browser has no Gamepad API or the page's
	// permissions policy blocks it, in which case every slot stays empty.
	available bool
}

func newGamepadReader(window js.Value) gamepadReader {
	g := gamepadReader{navigator: window.Get("navigator"), standard: js.ValueOf("standard")}
	g.available = g.navigator.Truthy() &&
		g.navigator.Get("getGamepads").Type() == js.TypeFunction && g.probe()
	return g
}

// probe calls getGamepads once, because a blocking permissions policy makes it
// throw rather than return an empty list.
func (g *gamepadReader) probe() (ok bool) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, isJS := failure.(js.Error); !isJS {
				panic(failure) //nolint:forbidigo // Only a JavaScript exception is expected here.
			}
			ok = false
		}
	}()
	g.navigator.Call("getGamepads")
	return true
}

func (g *gamepadReader) poll(l *loop.Loop) {
	if !g.available {
		return
	}
	pads := g.navigator.Call("getGamepads")
	for slot := range engine.MaxGamepads {
		var pad engine.Gamepad
		if p := pads.Index(slot); p.Truthy() && p.Get("connected").Bool() &&
			p.Get("mapping").Equal(g.standard) {
			pad = readGamepad(p)
		}
		l.SetGamepad(slot, pad)
	}
}

// readGamepad reads a standard-layout pad. Buttons 0 to 15 are the engine's
// bits, and buttons 6 and 7 also carry the analog trigger values.
func readGamepad(p js.Value) engine.Gamepad {
	buttons, axes := p.Get("buttons"), p.Get("axes")
	var bits engine.GamepadButtons
	var leftTrigger, rightTrigger float64
	for i := range min(buttons.Length(), 16) {
		button := buttons.Index(i)
		if button.Get("pressed").Bool() {
			bits |= 1 << i
		}
		switch i {
		case 6:
			leftTrigger = button.Get("value").Float()
		case 7:
			rightTrigger = button.Get("value").Float()
		}
	}
	var sticks [4]float64
	for i := range min(axes.Length(), len(sticks)) {
		sticks[i] = axes.Index(i).Float()
	}
	return loop.NormalizeGamepad(bits, sticks[0], sticks[1], sticks[2], sticks[3],
		leftTrigger, rightTrigger)
}

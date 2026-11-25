//go:build wasm

// Package browser runs Goje on a focusable HTML canvas using WebGL 2. It owns
// browser events and presentation; the shared driver owns pacing and drawing.
package browser

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

var errContextLost = errors.New("browser: WebGL 2 context lost")

// Run enters a scene on the canvas with ID goje and blocks until cancellation
// or a fatal error. The caller provides HTML and the Go WebAssembly bootstrap.
func Run(ctx context.Context, config driver.Config, factory engine.SceneFactory) error {
	window := js.Global().Get("window")
	if !window.Truthy() || !window.Get("document").Truthy() {
		return errors.New("browser: a window with a document is required")
	}
	document := window.Get("document")
	canvas := document.Call("getElementById", "goje")
	if !canvas.Truthy() || !canvas.InstanceOf(window.Get("HTMLCanvasElement")) {
		return errors.New(`browser: expected a canvas with id="goje"`)
	}
	gl, err := webGLContext(canvas)
	if err != nil {
		return err
	}
	graphics, err := newRenderer(gl, config)
	if err != nil {
		return err
	}
	defer graphics.close()

	if ctx.Err() != nil {
		return nil
	}
	// Prepare clips before the first tick; a missing backend leaves the queue disabled.
	sounds := newWebAudio(ctx, window, config.Sound, config.Logger)
	defer sounds.close()
	session, err := driver.Start(ctx, factory, config)
	if err != nil {
		return fmt.Errorf("browser: %w", err)
	}
	return runLoop(ctx, window, document, canvas, session, graphics, sounds)
}

func webGLContext(canvas js.Value) (gl js.Value, err error) {
	// A canvas transferred to an OffscreenCanvas throws instead of returning null.
	defer func() {
		if failure := recover(); failure != nil {
			if exception, ok := failure.(js.Error); ok {
				err = fmt.Errorf("browser: get WebGL 2 context: %w", exception)
				return
			}
			panic(failure) //nolint:forbidigo // Unexpected Go panics are not WebGL startup errors.
		}
	}()
	gl = canvas.Call("getContext", "webgl2", map[string]any{"alpha": false})
	if !gl.Truthy() {
		return js.Undefined(), errors.New("browser: WebGL 2 context unavailable")
	}
	return gl, nil
}

func runLoop(
	ctx context.Context,
	window, document, canvas js.Value,
	session *driver.Session,
	graphics *renderer,
	sounds *webAudio,
) (err error) {
	updateActive := func() {
		session.SetActive(!document.Get("hidden").Bool() &&
			document.Call("hasFocus").Bool() && document.Get("activeElement").Equal(canvas))
		// Frames may stop while hidden, so apply a cleared queue now.
		sounds.poll()
	}
	lost := make(chan struct{}, 1)
	focusCanvas := func() {
		// Focus before buffering so a valid press can reactivate the game.
		if !document.Get("activeElement").Equal(canvas) {
			canvas.Call("focus", map[string]any{"preventScroll": true})
			updateActive()
		}
	}
	event := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		kind := e.Get("type").String()
		switch kind {
		case "pointerdown", "pointermove", "pointerup", "pointercancel":
			if e.Get("pointerType").String() != "touch" {
				break
			}
			e.Call("preventDefault")
			if kind == "pointerdown" {
				focusCanvas()
			} else if kind == "pointerup" && e.Get("isTrusted").Bool() {
				// Touch activation is granted on release, not on press.
				sounds.unlock(window)
			}
			graphics.touchEvent(kind, window, canvas, e, &session.Loop)
			return nil
		}
		switch kind {
		case "blur":
			session.SetActive(false)
			sounds.poll()
		case "focus", "visibilitychange":
			updateActive()
		case "keydown", "keyup":
			if kind == "keydown" && !e.Get("repeat").Bool() && e.Get("isTrusted").Bool() {
				sounds.unlock(window)
			}
			if key := gameKey(e.Get("code").String()); key != 0 {
				if key == engine.KeyTab && kind == "keydown" &&
					(e.Get("shiftKey").Bool() || e.Get("ctrlKey").Bool() ||
						e.Get("altKey").Bool() || e.Get("metaKey").Bool()) {
					break // Modified Tab still navigates browser focus and tabs.
				}
				e.Call("preventDefault")
				switch {
				case key == engine.KeyShift || key == engine.KeyControl:
					// Either physical key holds the logical key, which the event's
					// modifier flag already reports.
					flag := "shiftKey"
					if key == engine.KeyControl {
						flag = "ctrlKey"
					}
					if e.Get(flag).Bool() {
						session.Loop.KeyDown(key, false)
					} else {
						session.Loop.KeyUp(key)
					}
				case kind == "keydown":
					session.Loop.KeyDown(key, e.Get("repeat").Bool())
				default:
					session.Loop.KeyUp(key)
				}
			}
		case "pointerdown", "mousedown":
			button := e.Get("button").Int()
			if button != 0 && button != 2 {
				break
			}
			if kind == "pointerdown" {
				if e.Get("pointerType").String() != "mouse" {
					break
				}
				if e.Get("isTrusted").Bool() {
					sounds.unlock(window)
				}
			} else if mask := 1 << (button / 2); e.Get("buttons").Int()&^mask == 0 {
				// Pointerdown reports the first physical button. Mousedown
				// reports additional buttons without observing pointer motion.
				break
			}
			x, y, ok := graphics.logicalPointer(window, canvas, e)
			if !ok {
				break
			}
			focusCanvas()
			if kind == "pointerdown" {
				// Capture so the release and the drag's motion arrive even when the
				// pointer leaves the canvas.
				capturePointer(canvas, e)
			}
			if button == 0 {
				session.Loop.PressPrimary(x, y)
			} else {
				session.Loop.PressSecondary(x, y)
			}
		case "pointermove":
			if e.Get("pointerType").String() == "mouse" {
				x, y, ok := graphics.logicalPointer(window, canvas, e)
				session.Loop.MovePointer(x, y, ok)
			}
		case "pointerleave":
			if e.Get("pointerType").String() == "mouse" &&
				!canvas.Call("hasPointerCapture", e.Get("pointerId")).Bool() {
				session.Loop.MovePointer(0, 0, false)
			}
		case "mouseup":
			switch e.Get("button").Int() {
			case 0:
				session.Loop.ReleasePrimary()
			case 2:
				session.Loop.ReleaseSecondary()
			}
		case "pointerup":
			// Pen activation is granted on release, not on press.
			if e.Get("pointerType").String() != "mouse" && e.Get("isTrusted").Bool() {
				sounds.unlock(window)
			}
		case "contextmenu":
			e.Call("preventDefault")
		case "webglcontextlost":
			select {
			case lost <- struct{}{}:
			default:
			}
		}
		return nil
	})
	listeners := [...]struct {
		target js.Value
		name   string
	}{
		{canvas, "keydown"},
		{canvas, "keyup"},
		{canvas, "pointerdown"},
		{canvas, "pointerup"},
		{canvas, "pointercancel"},
		{canvas, "mousedown"},
		{canvas, "mouseup"},
		{canvas, "pointermove"},
		{canvas, "pointerleave"},
		{canvas, "contextmenu"},
		{canvas, "focus"},
		{canvas, "blur"},
		{window, "focus"},
		{window, "blur"},
		{document, "visibilitychange"},
		{canvas, "webglcontextlost"},
	}

	// Hold the JS callback open until the Go loop finishes presenting. This puts
	// drawing before the browser's paint and keeps scene exit on the Go loop,
	// including when the context is cancelled during a scene callback.
	frames := make(chan time.Duration)
	presented := make(chan struct{})
	stopped := make(chan struct{})
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any {
		stamp := time.Duration(args[0].Float() * float64(time.Millisecond))
		select {
		case frames <- stamp:
			select {
			case <-presented:
			case <-stopped:
			}
		case <-stopped:
		}
		return nil
	})
	var pending js.Value
	addedTabIndex := false
	style := canvas.Get("style")
	touchAction := style.Get("touchAction")
	defer func() {
		close(stopped)
		if stopErr := session.Stop(); stopErr != nil {
			err = errors.Join(err, fmt.Errorf("browser: stop scene: %w", stopErr))
		}
		if !pending.IsUndefined() {
			window.Call("cancelAnimationFrame", pending)
		}
		for _, listener := range &listeners {
			listener.target.Call("removeEventListener", listener.name, event)
		}
		frame.Release()
		event.Release()
		if addedTabIndex {
			canvas.Call("removeAttribute", "tabindex")
		}
		style.Set("touchAction", touchAction)
	}()
	for _, listener := range &listeners {
		listener.target.Call("addEventListener", listener.name, event)
	}
	if graphics.gl.Call("isContextLost").Bool() {
		return errContextLost
	}
	if !canvas.Call("hasAttribute", "tabindex").Bool() {
		canvas.Set("tabIndex", 0)
		addedTabIndex = true
	}
	// Without this, a drag scrolls or zooms the page instead of reaching the game.
	style.Set("touchAction", "none")
	pads := newGamepadReader(window)
	// Don't take focus from a control the page or the user already selected.
	if active := document.Get(
		"activeElement",
	); active.Equal(document.Get("body")) ||
		active.Equal(document.Get("documentElement")) {
		canvas.Call("focus", map[string]any{"preventScroll": true})
	}
	updateActive()
	pinIntrinsicSize(canvas)

	done := ctx.Done()
	for {
		pending = window.Call("requestAnimationFrame", frame)
		select {
		case <-done:
			return nil
		case <-lost:
			return errContextLost
		case stamp := <-frames:
			pending = js.Undefined()
			measured := driver.BenchFrameStart()
			pads.poll(&session.Loop)
			batch, err := session.Advance(stamp)
			if err != nil {
				return fmt.Errorf("browser: %w", err)
			}
			sounds.poll()
			submission := session.StartSubmission()
			drawErr := graphics.present(window, canvas, batch)
			session.FinishSubmission(submission)
			if drawErr != nil {
				return drawErr
			}
			driver.BenchFrameEnd(measured, len(batch.Vertices))
			session.Presented()
			presented <- struct{}{}
		}
	}
}

func (r *renderer) logicalPointer(window, canvas, event js.Value) (x, y float64, ok bool) {
	// The client rect and pointer share the browser viewport's origin, even
	// when the page is scrolled. The drawing buffer covers the CSS content box.
	box := canvas.Call("getBoundingClientRect")
	style := window.Call("getComputedStyle", canvas)
	left := cssInset(style, "borderLeftWidth") + cssInset(style, "paddingLeft")
	right := cssInset(style, "borderRightWidth") + cssInset(style, "paddingRight")
	top := cssInset(style, "borderTopWidth") + cssInset(style, "paddingTop")
	bottom := cssInset(style, "borderBottomWidth") + cssInset(style, "paddingBottom")
	return driver.LogicalPointer(
		event.Get("clientX").Float()-box.Get("left").Float()-left,
		event.Get("clientY").Float()-box.Get("top").Float()-top,
		box.Get("width").Float()-left-right,
		box.Get("height").Float()-top-bottom,
		r.framebuffer,
		image.Pt(r.logicalW, r.logicalH),
		r.viewport,
	)
}

func cssInset(style js.Value, property string) float64 {
	// Computed border and padding lengths are expressed in CSS pixels.
	value, err := strconv.ParseFloat(strings.TrimSuffix(style.Get(property).String(), "px"), 64)
	if err != nil {
		return math.NaN() // An unknown CSS box cannot supply a valid press.
	}
	return value
}

func gameKey(code string) engine.Keys {
	switch code {
	case "ArrowLeft":
		return engine.KeyArrowLeft
	case "ArrowRight":
		return engine.KeyArrowRight
	case "ArrowUp":
		return engine.KeyArrowUp
	case "ArrowDown":
		return engine.KeyArrowDown
	case "KeyA":
		return engine.KeyA
	case "KeyD":
		return engine.KeyD
	case "KeyW":
		return engine.KeyW
	case "KeyS":
		return engine.KeyS
	case "KeyR":
		return engine.KeyR
	case "Space":
		return engine.KeySpace
	case "ShiftLeft", "ShiftRight":
		return engine.KeyShift
	case "ControlLeft", "ControlRight":
		return engine.KeyControl
	case "Escape":
		return engine.KeyEscape
	case "Enter", "NumpadEnter":
		return engine.KeyEnter
	case "KeyH":
		return engine.KeyH
	case "KeyF":
		return engine.KeyF
	case "Tab":
		return engine.KeyTab
	case "Digit1", "Digit2", "Digit3", "Digit4", "Digit5", "Digit6", "Digit7", "Digit8", "Digit9":
		return engine.Key1 << (code[len("Digit")] - '1')
	default:
		return 0
	}
}

// capturePointer routes the rest of a mouse drag to the canvas. A synthetic or
// expired pointer ID makes the call throw, and the game plays on without capture.
func capturePointer(canvas, e js.Value) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(js.Error); !ok {
				panic(failure) //nolint:forbidigo // Only a JavaScript exception is expected here.
			}
		}
	}()
	canvas.Call("setPointerCapture", e.Get("pointerId"))
}

//go:build wasm

// Package browser runs Goje on a focusable HTML canvas using WebGL 2. It owns
// browser events and presentation; the shared driver owns pacing and drawing.
package browser

import (
	"context"
	"errors"
	"fmt"
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
	session, err := driver.Start(ctx, factory, config)
	if err != nil {
		return fmt.Errorf("browser: %w", err)
	}
	return runLoop(ctx, window, document, canvas, session, graphics)
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
) (err error) {
	updateActive := func() {
		session.SetActive(!document.Get("hidden").Bool() &&
			document.Call("hasFocus").Bool() && document.Get("activeElement").Equal(canvas))
	}
	lost := make(chan struct{}, 1)
	event := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		kind := e.Get("type").String()
		switch kind {
		case "blur":
			session.SetActive(false)
		case "focus", "visibilitychange":
			updateActive()
		case "keydown", "keyup":
			if key := gameKey(e.Get("code").String()); key != 0 {
				e.Call("preventDefault")
				if kind == "keydown" {
					session.Loop.KeyDown(key, e.Get("repeat").Bool())
				} else {
					session.Loop.KeyUp(key)
				}
			}
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
	defer func() {
		close(stopped)
		if stopErr := session.Stop(); stopErr != nil {
			err = errors.Join(err, fmt.Errorf("browser: stop scene: %w", stopErr))
		}
		if !pending.IsUndefined() {
			window.Call("cancelAnimationFrame", pending)
		}
		for _, listener := range listeners {
			listener.target.Call("removeEventListener", listener.name, event)
		}
		frame.Release()
		event.Release()
		if addedTabIndex {
			canvas.Call("removeAttribute", "tabindex")
		}
	}()
	for _, listener := range listeners {
		listener.target.Call("addEventListener", listener.name, event)
	}
	if graphics.gl.Call("isContextLost").Bool() {
		return errContextLost
	}
	if !canvas.Call("hasAttribute", "tabindex").Bool() {
		canvas.Set("tabIndex", 0)
		addedTabIndex = true
	}
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
			batch, err := session.Advance(stamp)
			if err != nil {
				return fmt.Errorf("browser: %w", err)
			}
			submission := session.StartSubmission()
			drawErr := graphics.present(window, canvas, batch)
			session.FinishSubmission(submission)
			if drawErr != nil {
				return drawErr
			}
			driver.BenchFrameEnd(measured)
			session.Presented()
			presented <- struct{}{}
		}
	}
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
	default:
		return 0
	}
}

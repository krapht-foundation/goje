//go:build cgo && (darwin || linux)

// Package desktop connects the shared game loop to a GLFW window and an OpenGL
// context. GLFW owns window events; tick scheduling and frame packing stay in
// the shared platform driver.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

// When SwapBuffers does not block (for example under Xvfb), cap presentation
// without imposing a 60 Hz render cadence on a higher-refresh display.
const minFrameInterval = time.Second / 240

// Package initialization runs on the startup thread. Lock it before main so
// GLFW can use that thread for window creation, events, and the GL context.
//
//nolint:gochecknoinits // Locking a thread in Run cannot pin it to the startup thread.
func init() {
	runtime.LockOSThread()
}

// Run owns one window and runs a scene until close, cancellation, or failure.
// Call it from main, not a new goroutine: GLFW requires the startup OS thread.
func Run(ctx context.Context, config driver.Config, factory engine.SceneFactory) (err error) {
	if initErr := initGLFW(); initErr != nil {
		return initErr
	}
	defer glfw.Terminate()

	window, err := openWindow(config)
	if err != nil {
		return err
	}
	defer window.Destroy()

	graphics, err := newRenderer(config)
	if err != nil {
		return err
	}
	defer graphics.close()
	if ctx.Err() != nil {
		return nil
	}
	// Disabled audio does no device work. Opened before the session so its
	// callbacks stop only after the last tick.
	if config.Sound != nil {
		defer openSpeaker(ctx, config.Logger, config.Sound).close()
	}

	session, err := driver.Start(ctx, factory, config)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Stop())
	}()

	return runWindow(ctx, config, window, graphics, session)
}

func openWindow(config driver.Config) (*glfw.Window, error) {
	glfw.DefaultWindowHints()
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	// Cocoa only exposes OpenGL 3.2 and up as a forward-compatible core context.
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	// Request reset notification on drivers that expose robust contexts.
	glfw.WindowHint(glfw.ContextRobustness, glfw.LoseContextOnReset)
	glfw.WindowHint(glfw.Resizable, glfw.True)
	window, err := glfw.CreateWindow(2*config.Width, 2*config.Height, config.Title, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create GLFW window: %w", err)
	}
	if window == nil {
		return nil, fmt.Errorf("create GLFW window: %w", glfwFailure())
	}

	window.MakeContextCurrent()
	if glfw.GetCurrentContext() != window {
		err := nativeGLFWError()
		window.Destroy()
		if err != nil {
			return nil, fmt.Errorf("make OpenGL context current: %w", err)
		}
		return nil, errors.New("GLFW did not make the OpenGL context current")
	}
	if err := gl.InitWithProcAddrFunc(glfw.GetProcAddress); err != nil {
		window.Destroy()
		return nil, fmt.Errorf("load OpenGL 3.3 core: %w", err)
	}
	glfw.SwapInterval(1)
	return window, nil
}

func runWindow(
	ctx context.Context,
	config driver.Config,
	window *glfw.Window,
	graphics *renderer,
	session *driver.Session,
) error {
	width, height := window.GetFramebufferSize()
	viewport := driver.Viewport(width, height, config.Width, config.Height)
	needsDraw := true
	keepActive := driver.BenchKeepActive() || probeKeepsActive()
	focused := keepActive || window.GetAttrib(glfw.Focused) == glfw.True
	session.SetActive(focused && !viewport.Empty())
	window.SetFocusCallback(func(_ *glfw.Window, value bool) {
		focused = keepActive || value
		session.SetActive(focused && !viewport.Empty())
	})
	window.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		needsDraw = true
		width, height = w, h
		viewport = driver.Viewport(width, height, config.Width, config.Height)
		session.SetActive(focused && !viewport.Empty())
	})
	window.SetRefreshCallback(func(_ *glfw.Window) { needsDraw = true })
	window.SetKeyCallback(func(
		w *glfw.Window,
		key glfw.Key,
		_ int,
		action glfw.Action,
		_ glfw.ModifierKey,
	) {
		mapped := engineKey(key)
		if mapped == 0 {
			return
		}
		if down, isModifier := modifierDown(w, mapped); isModifier {
			if down {
				session.Loop.KeyDown(mapped, false)
			} else {
				session.Loop.KeyUp(mapped)
			}
			return
		}
		switch action {
		case glfw.Press, glfw.Repeat:
			session.Loop.KeyDown(mapped, action == glfw.Repeat)
		case glfw.Release:
			session.Loop.KeyUp(mapped)
		}
	})
	// Follow GLFW's ordered cursor events. Querying the live cursor from a
	// button callback can see movement newer than the queued button event.
	cursorX, cursorY := window.GetCursorPos()
	windowX, windowY := window.GetPos()
	logicalCursor := func(w *glfw.Window) (x, y float64, ok bool) {
		windowW, windowH := w.GetSize()
		return driver.LogicalPointer(cursorX, cursorY, float64(windowW), float64(windowH),
			image.Pt(width, height), image.Pt(config.Width, config.Height), viewport)
	}
	window.SetCursorPosCallback(func(w *glfw.Window, x, y float64) {
		cursorX, cursorY = x, y
		px, py, ok := logicalCursor(w)
		session.Loop.MovePointer(px, py, ok)
	})
	window.SetCursorEnterCallback(func(_ *glfw.Window, entered bool) {
		if !entered {
			session.Loop.MovePointer(0, 0, false)
		}
	})
	window.SetPosCallback(func(_ *glfw.Window, x, y int) {
		cursorX += float64(windowX - x)
		cursorY += float64(windowY - y)
		windowX, windowY = x, y
	})
	window.SetMouseButtonCallback(
		func(w *glfw.Window, button glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
			if button != glfw.MouseButtonLeft && button != glfw.MouseButtonRight {
				return
			}
			if action == glfw.Release {
				if button == glfw.MouseButtonLeft {
					session.Loop.ReleasePrimary()
				} else {
					session.Loop.ReleaseSecondary()
				}
				return
			}
			x, y, ok := logicalCursor(w)
			if action != glfw.Press || !ok {
				return
			}
			if button == glfw.MouseButtonLeft {
				session.Loop.PressPrimary(x, y)
			} else {
				session.Loop.PressSecondary(x, y)
			}
		},
	)

	// Wake a suspended event wait on cancellation. Join the wakeup before
	// the window and GLFW are destroyed so it cannot post to a closed host.
	wakeupDone := make(chan struct{})
	stopWakeup := context.AfterFunc(ctx, func() {
		defer close(wakeupDone)
		glfw.PostEmptyEvent()
	})
	defer func() {
		if !stopWakeup() {
			<-wakeupDone
		}
	}()

	start := time.Now()
	var nextFrame time.Time
	glfw.PollEvents()
	for ctx.Err() == nil && !window.ShouldClose() {
		if viewport.Empty() || (!focused && !needsDraw) {
			nextFrame = time.Time{}
			glfw.WaitEvents()
			continue
		}

		now := time.Now()
		if delay := nextFrame.Sub(now); delay > 0 {
			glfw.WaitEventsTimeout(delay.Seconds())
			continue
		}
		nextFrame = now.Add(minFrameInterval)

		measured := driver.BenchFrameStart()
		frame, err := session.Advance(now.Sub(start))
		if err != nil {
			return err
		}
		submission := session.StartSubmission()
		drawErr := graphics.draw(frame, viewport)
		session.FinishSubmission(submission)
		if drawErr != nil {
			return drawErr
		}
		driver.BenchFrameEnd(measured, len(frame.Vertices))
		if err := afterDraw(viewport); err != nil {
			return err
		}
		needsDraw = false
		window.SwapBuffers()
		session.Presented()
		glfw.PollEvents()
	}
	return nil
}

func engineKey(key glfw.Key) engine.Keys {
	switch key {
	case glfw.KeyLeft:
		return engine.KeyArrowLeft
	case glfw.KeyRight:
		return engine.KeyArrowRight
	case glfw.KeyUp:
		return engine.KeyArrowUp
	case glfw.KeyDown:
		return engine.KeyArrowDown
	case glfw.KeyA:
		return engine.KeyA
	case glfw.KeyD:
		return engine.KeyD
	case glfw.KeyW:
		return engine.KeyW
	case glfw.KeyS:
		return engine.KeyS
	case glfw.KeyR:
		return engine.KeyR
	case glfw.KeySpace:
		return engine.KeySpace
	case glfw.KeyLeftShift, glfw.KeyRightShift:
		return engine.KeyShift
	case glfw.KeyLeftControl, glfw.KeyRightControl:
		return engine.KeyControl
	case glfw.KeyEscape:
		return engine.KeyEscape
	case glfw.KeyEnter, glfw.KeyKPEnter:
		return engine.KeyEnter
	case glfw.KeyH:
		return engine.KeyH
	case glfw.KeyF:
		return engine.KeyF
	case glfw.Key1, glfw.Key2, glfw.Key3, glfw.Key4, glfw.Key5,
		glfw.Key6, glfw.Key7, glfw.Key8, glfw.Key9:
		return engine.Key1 << (key - glfw.Key1)
	default:
		return 0
	}
}

// modifierDown reports whether either physical key of a modifier is down.
// Either one holds the logical key, so a release counts only when the other is
// up too. It reports isModifier false for every other key.
func modifierDown(w *glfw.Window, key engine.Keys) (down, isModifier bool) {
	switch key {
	case engine.KeyShift:
		return w.GetKey(glfw.KeyLeftShift) == glfw.Press ||
			w.GetKey(glfw.KeyRightShift) == glfw.Press, true
	case engine.KeyControl:
		return w.GetKey(glfw.KeyLeftControl) == glfw.Press ||
			w.GetKey(glfw.KeyRightControl) == glfw.Press, true
	default:
		return false, false
	}
}

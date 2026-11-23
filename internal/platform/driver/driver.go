// Package driver connects the common host policy to the runtime and prepares
// draw batches for OpenGL and WebGL. A host owns the session and GPU resources;
// scenes only see the engine's logical canvas.
package driver

import (
	"context"
	"image"
	"log/slog"
	"time"

	"github.com/krapht-foundation/goje/diagnostics"
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/loop"
)

// Config describes validated host startup settings. Texture pixels are tightly
// packed from the upper-left corner; even an untextured game has a 1×1 atlas so
// that both graphics APIs can use the same shader and vertex layout.
type Config struct {
	Title         string
	Width, Height int
	Texture       *image.NRGBA
	HasImage      bool
	Logger        *slog.Logger
	Capture       *Capture
}

// Session owns the fixed-step policy, runtime, and reusable frame batch.
// All calls happen on the host loop.
type Session struct {
	Loop    loop.Loop
	runtime *engine.Runtime
	frame   Frame
	capture *Capture
	logger  *slog.Logger
	ctx     context.Context
}

// Start enters the first scene after the host has initialized its resources.
func Start(ctx context.Context, factory engine.SceneFactory, config Config) (*Session, error) {
	r, err := engine.StartWithLogger(ctx, factory, config.Logger)
	if err != nil {
		return nil, err
	}
	return &Session{
		runtime: r,
		frame:   Frame{hasImage: config.HasImage},
		capture: config.Capture,
		logger:  config.Logger,
		ctx:     ctx,
	}, nil
}

// SetActive applies focus and visibility changes, logging only on a transition.
func (s *Session) SetActive(active bool) {
	if !s.Loop.SetActive(active) || s.logger == nil {
		return
	}
	if active {
		s.logger.InfoContext(s.ctx, "runtime resumed")
	} else {
		s.logger.InfoContext(s.ctx, "runtime suspended")
	}
}

// Advance runs the scheduled updates, then records one frame for presentation.
// The returned frame is valid until the next call to Advance.
func (s *Session) Advance(now time.Duration) (*Frame, error) {
	budget := s.Loop.Frame(now)
	BenchDropped(budget.Dropped)
	// Select the uninstrumented loop before any per-tick timing or counting.
	if s.capture != nil {
		return s.advanceWithCapture(budget)
	}
	for range budget.Updates {
		start := benchTickStart()
		err := s.runtime.Step(s.Loop.NextInput())
		benchTickEnd(start)
		if err != nil {
			return nil, err
		}
	}
	if err := s.recordFrame(); err != nil {
		return nil, err
	}
	return &s.frame, nil
}

func (s *Session) advanceWithCapture(budget loop.Budget) (*Frame, error) {
	c := s.capture
	c.frame++
	if budget.Dropped > 0 {
		c.report.DroppedTicks += uint64(budget.Dropped)
	}
	for range budget.Updates {
		measured := benchTickStart()
		input := s.Loop.NextInput()
		start := c.phaseStart()
		err := s.runtime.Step(input)
		c.phaseFinish(start, diagnostics.Update, c.report.Ticks+1)
		if err == nil {
			c.report.Ticks++
		}
		benchTickEnd(measured)
		if err != nil {
			return nil, err
		}
	}
	start := c.phaseStart()
	err := s.recordFrame()
	c.phaseFinish(start, diagnostics.Draw, 0)
	if err != nil {
		return nil, err
	}
	return &s.frame, nil
}

func (s *Session) recordFrame() error {
	s.frame.reset()
	return s.runtime.Draw(&s.frame)
}

// StartSubmission starts the CPU submission phase for the frame from Advance.
func (s *Session) StartSubmission() time.Time {
	if s.capture != nil {
		return s.capture.phaseStart()
	}
	return time.Time{}
}

// FinishSubmission completes an attempted CPU submission, even if it failed.
func (s *Session) FinishSubmission(start time.Time) {
	if s.capture != nil {
		s.capture.phaseFinish(start, diagnostics.Submit, 0)
	}
}

// Presented counts a frame after the host successfully submits or swaps it.
func (s *Session) Presented() {
	if s.capture != nil {
		s.capture.report.Frames++
	}
}

// Stop exits the scene before the host deletes textures or closes its context.
func (s *Session) Stop() error { return s.runtime.Stop() }

// Viewport centers the logical surface inside a physical framebuffer. A caller
// supplies positive logical dimensions. The rectangle uses GL's bottom origin.
func Viewport(w, h, logicalW, logicalH int) image.Rectangle {
	if w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	vw, vh := w, h
	if int64(w)*int64(logicalH) > int64(h)*int64(logicalW) {
		vw = max(1, int(int64(h)*int64(logicalW)/int64(logicalH)))
	} else {
		vh = max(1, int(int64(w)*int64(logicalH)/int64(logicalW)))
	}
	x, y := (w-vw)/2, (h-vh)/2
	return image.Rect(x, y, x+vw, y+vh)
}

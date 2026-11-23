// Package platform launches a Goje scene on the current graphical host.
// Games use the same entry point on desktop and in a WebAssembly build;
// headless tests can use the engine directly without importing this package.
package platform

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"log/slog"

	"github.com/krapht-foundation/goje/diagnostics"
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

// Config describes the initial window and logical viewport. Image is optional;
// the host uploads its pixels once, before entering the scene, and keeps the
// texture alive across scene changes. Canvas source regions are relative to the
// image's upper-left pixel. Do not modify the image concurrently with Run.
type Config struct {
	Title         string
	Width, Height int
	Image         image.Image
	// Logger receives lifecycle messages; nil disables logging.
	Logger *slog.Logger
	// Diagnostics collects counters and optionally a bounded trace. Nil disables
	// instrumentation. Use a separate value for each concurrently running host.
	Diagnostics *Diagnostics
}

// Diagnostics configures local capture and receives the result after Run returns.
// Do not access or modify it while Run is active. It is safe to retain an old
// Result across sequential runs; each run owns a fresh capture.
type Diagnostics struct {
	// TraceCapacity is the maximum number of retained records (0 through
	// 1,048,576). Zero collects counters without reading a profiling clock.
	TraceCapacity int
	// Result holds counters and the finished trace, including on a runtime error.
	Result diagnostics.Report
}

// Run opens the host and enters the scene. Call it from the main goroutine.
// Closing the window or cancelling ctx ends the loop and exits the active scene
// before releasing host resources. In a browser, provide a canvas with ID goje.
func Run(ctx context.Context, config Config, scene engine.SceneFactory) error {
	if config.Diagnostics != nil {
		config.Diagnostics.Result = diagnostics.Report{}
	}
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	// Logical dimensions are also doubled for the initial desktop window size.
	if config.Width <= 0 || config.Height <= 0 || config.Width > 1<<30-1 || config.Height > 1<<30-1 {
		return errors.New("logical viewport dimensions must be positive and fit in the initial window")
	}
	if scene == nil {
		return errors.New("initial scene factory must not be nil")
	}
	texture, err := packTexture(config.Image)
	if err != nil {
		return err
	}
	var capture *driver.Capture
	if config.Diagnostics != nil {
		capture, err = driver.NewCapture(config.Diagnostics.TraceCapacity)
		if err != nil {
			return err
		}
		defer func() { config.Diagnostics.Result = capture.Report() }()
	}
	if config.Logger != nil {
		config.Logger.InfoContext(ctx, "Goje host starting")
		defer config.Logger.InfoContext(ctx, "Goje host stopped")
	}
	var imageSize image.Point
	if config.Image != nil {
		imageSize = texture.Bounds().Size()
	}
	return run(ctx, driver.Config{
		Title: config.Title, Width: config.Width, Height: config.Height,
		Texture: texture, ImageSize: imageSize,
		Logger: config.Logger, Capture: capture,
	}, scene)
}

func packTexture(source image.Image) (*image.NRGBA, error) {
	if source == nil {
		texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		texture.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		return texture, nil
	}
	bounds := source.Bounds()
	if bounds.Empty() {
		return nil, errors.New("launch image must not be empty")
	}
	// Uploads need contiguous, straight-alpha bytes. Reuse an already packed
	// NRGBA image rather than normalizing it through a second full-size buffer.
	if packed, ok := source.(*image.NRGBA); ok && packed.Stride == 4*bounds.Dx() {
		return packed, nil
	}
	texture := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(texture, texture.Bounds(), source, bounds.Min, draw.Src)
	return texture, nil
}

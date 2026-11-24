// Package sprite loads game-owned PNG sheets and clip metadata and plays clips
// on fixed simulation ticks without owning a filesystem, clock, or renderer.
package sprite

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
)

const (
	maxPNGBytes = 1 << 20
	maxAxis     = 512
)

// Frame describes a source region in sheet pixels, its frame-relative pivot,
// and how many fixed simulation ticks to display it.
type Frame struct {
	Source image.Rectangle
	Pivot  image.Point
	Ticks  int
}

// Destination aligns the frame-relative pivot with a logical pixel anchor.
// Scale must be finite and positive and is logical pixels per sheet pixel.
// Use 1 for unscaled sprites or the camera zoom for projected sprites.
func (f Frame) Destination(anchorX, anchorY, scale float32) (x, y, width, height float32) {
	return anchorX - float32(f.Pivot.X)*scale, anchorY - float32(f.Pivot.Y)*scale,
		float32(f.Source.Dx()) * scale, float32(f.Source.Dy()) * scale
}

// Clip is a read-only sequence of frames in authored order.
type Clip struct {
	frames []Frame
}

// Len returns the number of frames.
func (c Clip) Len() int { return len(c.frames) }

// Frame returns the frame at i. The caller must pass an index in [0, Len()).
func (c Clip) Frame(i int) Frame { return c.frames[i] }

// InAtlas translates the clip's source rectangles to the sheet's position in
// a launch image. Prepare and reuse the result across actors at startup.
// A nonzero offset makes a separate clip and leaves the original unchanged.
func (c Clip) InAtlas(offset image.Point) Clip {
	if offset == (image.Point{}) || len(c.frames) == 0 {
		return c
	}
	frames := make([]Frame, len(c.frames))
	for i, frame := range c.frames {
		frame.Source = frame.Source.Add(offset)
		frames[i] = frame
	}
	return Clip{frames: frames}
}

// Sheet holds decoded pixels and named clips that may be shared across scenes.
type Sheet struct {
	pixels image.Image
	clips  map[string]Clip
}

// Image returns the sheet pixels. Callers must treat them as read-only.
func (s *Sheet) Image() image.Image { return s.pixels }

// Clip looks up a case-sensitive name. Resolve names during scene setup, not per tick.
func (s *Sheet) Clip(name string) (Clip, bool) {
	clip, ok := s.clips[name]
	return clip, ok
}

// Decode reads one sprite sheet v1 asset. It does not close the input readers.
// It returns a nil sheet if either input is invalid.
func Decode(sheetPNG, clipsJSON io.Reader) (*Sheet, error) {
	data, err := readBounded(sheetPNG, maxPNGBytes)
	if err != nil {
		return nil, fmt.Errorf("read sprite PNG: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read sprite PNG dimensions: %w", err)
	}
	if config.Width < 1 || config.Width > maxAxis || config.Height < 1 || config.Height > maxAxis {
		return nil, fmt.Errorf("sprite PNG axes must be 1–%d pixels", maxAxis)
	}
	clips, err := decodeClips(clipsJSON, image.Pt(config.Width, config.Height))
	if err != nil {
		return nil, fmt.Errorf("sprite clips: %w", err)
	}
	// Decode the same bounded bytes so the checked header cannot change.
	pixels, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode sprite PNG: %w", err)
	}
	return &Sheet{pixels: pixels, clips: clips}, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}

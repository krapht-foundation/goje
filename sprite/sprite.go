// Package sprite loads game-owned PNG sheets and clip metadata and plays clips
// on fixed simulation ticks without owning a filesystem, clock, or renderer.
package sprite

import (
	"fmt"
	"image"
	"io"

	"github.com/krapht-foundation/goje/internal/spriteformat"
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
	frames []spriteformat.Frame
}

// Len returns the number of frames.
func (c Clip) Len() int { return len(c.frames) }

// Frame returns the frame at i. The caller must pass an index in [0, Len()).
func (c Clip) Frame(i int) Frame { return Frame(c.frames[i]) }

// InAtlas translates the clip's source rectangles to the sheet's position in
// a launch image. Prepare and reuse the result across actors at startup.
// A nonzero offset makes a separate clip and leaves the original unchanged.
func (c Clip) InAtlas(offset image.Point) Clip {
	if offset == (image.Point{}) || len(c.frames) == 0 {
		return c
	}
	frames := make([]spriteformat.Frame, len(c.frames))
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
	png, err := spriteformat.ReadPNG(sheetPNG)
	if err != nil {
		return nil, err
	}
	clips := make(map[string]Clip)
	if decodeErr := spriteformat.DecodeClips(clipsJSON, png.Size(), func(clip spriteformat.Clip) {
		clips[clip.Name] = Clip{frames: clip.Frames}
	}); decodeErr != nil {
		return nil, fmt.Errorf("sprite clips: %w", decodeErr)
	}
	pixels, err := png.Decode()
	if err != nil {
		return nil, err
	}
	return &Sheet{pixels: pixels, clips: clips}, nil
}

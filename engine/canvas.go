package engine

import (
	"image"
	"image/color"
)

// Canvas records drawing in logical pixels from the upper-left corner. Draw
// calls are painted in order, except Clear, which sets the frame background.
// A scene must not retain the canvas after its Draw callback returns.
type Canvas interface {
	Clear(color.NRGBA)
	Rect(x, y, width, height float32, tint color.NRGBA)
	// Line draws a centered stroke with flat ends. Zero-length lines are ignored.
	Line(x1, y1, x2, y2, width float32, tint color.NRGBA)
	// Image draws a region of the launch image with nearest-neighbor scaling.
	// Source coordinates are relative to the upper-left texel, with exclusive
	// maximum edges. Missing images and empty or out-of-bounds regions do not draw.
	Image(source image.Rectangle, x, y, width, height float32)
	// ImageTinted is Image with every texel multiplied by the tint, so one white
	// glyph sheet can be drawn in any color. Opaque white draws the texels unchanged.
	ImageTinted(source image.Rectangle, x, y, width, height float32, tint color.NRGBA)
}

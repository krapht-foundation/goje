package engine

import "image/color"

// Canvas records drawing in logical pixels from the upper-left corner. Draw
// calls are painted in order, except Clear, which sets the frame background.
// A scene must not retain the canvas after its Draw callback returns.
type Canvas interface {
	Clear(color.NRGBA)
	Rect(x, y, width, height float32, tint color.NRGBA)
	// Line draws a centered stroke with flat ends. Zero-length lines are ignored.
	Line(x1, y1, x2, y2, width float32, tint color.NRGBA)
	// Image draws the image configured at launch with nearest-neighbor scaling.
	// It draws nothing if the game did not configure an image.
	Image(x, y, width, height float32)
}

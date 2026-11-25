//go:build goje_smoke && cgo && (darwin || linux || windows)

package desktop

import (
	"image"
	"image/color"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// FrameProbe inspects the completed GL back buffer on the host thread before
// swap. The smoke command installs it before Run; regular builds exclude it.
var FrameProbe func(viewport image.Rectangle) error

// Scripted framebuffer checks do not depend on which native window has focus.
func probeKeepsActive() bool { return FrameProbe != nil }

func afterDraw(viewport image.Rectangle) error {
	if FrameProbe != nil {
		return FrameProbe(viewport)
	}
	return nil
}

// ProbePixel reads the center of a logical pixel from the GL back buffer.
// Call it only from a frame probe on the host thread.
func ProbePixel(viewport image.Rectangle, logical, point image.Point) color.NRGBA {
	x := viewport.Min.X + (2*point.X+1)*viewport.Dx()/(2*logical.X)
	y := viewport.Max.Y - 1 - (2*point.Y+1)*viewport.Dy()/(2*logical.Y)
	var pixel [4]byte
	//nolint:gosec // G115: The sample is inside the GLFW framebuffer's C int bounds.
	gl.ReadPixels(int32(x), int32(y), 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&pixel[0]))
	return color.NRGBA{R: pixel[0], G: pixel[1], B: pixel[2], A: pixel[3]}
}

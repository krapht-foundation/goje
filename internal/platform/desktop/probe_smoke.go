//go:build goje_smoke && cgo && (darwin || linux)

package desktop

import "image"

// FrameProbe inspects the completed GL back buffer on the host thread before
// swap. The smoke command installs it before Run; regular builds exclude it.
var FrameProbe func(viewport image.Rectangle) error

func afterDraw(viewport image.Rectangle) error {
	if FrameProbe != nil {
		return FrameProbe(viewport)
	}
	return nil
}

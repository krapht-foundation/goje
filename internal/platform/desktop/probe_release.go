//go:build !goje_smoke && cgo && (darwin || linux)

package desktop

import "image"

// The compiler inlines this no-op in regular builds; no hook runs per frame.
func afterDraw(image.Rectangle) error { return nil }

func probeKeepsActive() bool { return false }

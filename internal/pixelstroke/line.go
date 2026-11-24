// Package pixelstroke defines pixel-grid interpolation shared by the art editors.
package pixelstroke

import "image"

// Line visits the integer pixels after from through to. The caller has already
// painted from and checked that both ends lie on its canvas.
func Line(from, to image.Point, paint func(image.Point)) {
	dx, sx := to.X-from.X, 1
	if dx < 0 {
		dx, sx = -dx, -1
	}
	dy, sy := to.Y-from.Y, 1
	if dy < 0 {
		dy, sy = -dy, -1
	}
	balance := dx - dy
	for from != to {
		twice := 2 * balance
		if twice > -dy {
			balance -= dy
			from.X += sx
		}
		if twice < dx {
			balance += dx
			from.Y += sy
		}
		paint(from)
	}
}

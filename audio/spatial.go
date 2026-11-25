package audio

import "math"

// Spatialize returns the gain and pan of a sound at source heard from
// listener. The gain falls linearly from 1 at the listener to 0 at hearing
// units away. The pan is the horizontal offset divided by half of hearing,
// clamped to -1 (left) and 1 (right). Games choose the units, usually logical
// screen pixels. A hearing that is not positive, a source out of hearing, and
// a NaN argument give a gain and pan of 0. It is pure and allocates nothing.
func Spatialize(listenerX, listenerY, sourceX, sourceY, hearing float64) (gain, pan float32) {
	if !(hearing > 0) {
		return 0, 0
	}
	dx := sourceX - listenerX
	g := 1 - math.Hypot(dx, sourceY-listenerY)/hearing
	// The negated comparison also rejects NaN, which Hypot returns for NaN input.
	if !(g > 0) {
		return 0, 0
	}
	return float32(g), float32(max(-1, min(1, 2*dx/hearing)))
}

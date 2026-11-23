package engine

// Keys is a bit set of keyboard keys. Combine keys with the | operator.
type Keys uint16

const (
	KeyArrowLeft Keys = 1 << iota
	KeyArrowRight
	KeyArrowUp
	KeyArrowDown
	KeyA
	KeyD
	KeyW
	KeyS
	KeyR
	KeySpace
)

// Has reports whether all the specified keys are in the set.
func (k Keys) Has(keys Keys) bool { return k&keys == keys }

// MousePress is the first primary mouse-button press buffered for a tick.
// X and Y are logical viewport pixels measured from the top left. Read them
// only when Valid is true; a press at (0, 0) is valid.
type MousePress struct {
	X, Y  float64
	Valid bool
}

// Input is a value snapshot for one simulation tick. Held describes keys down
// at tick start, while Pressed includes new key presses since the previous tick.
// A short press can appear in Pressed without appearing in Held.
// PrimaryPress is consumed once, regardless of the number of updates per frame.
type Input struct {
	Held         Keys
	Pressed      Keys
	PrimaryPress MousePress
}

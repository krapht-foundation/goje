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
)

// Has reports whether all the specified keys are in the set.
func (k Keys) Has(keys Keys) bool { return k&keys == keys }

// Input is a value snapshot for one simulation tick. Held describes keys down
// at tick start, while Pressed includes new key presses since the previous tick.
// A short press can appear in Pressed without appearing in Held.
type Input struct {
	Held    Keys
	Pressed Keys
}

package engine

// Keys is a bit set of keyboard keys. Combine keys with the | operator.
type Keys uint32

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
	// KeyShift and KeyControl are down while either physical key is down.
	KeyShift
	KeyControl
	KeyEscape
	KeyEnter
	KeyH
	KeyF
	// Key1 through Key9 are the digits on the main row of the keyboard.
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
)

// Has reports whether all the specified keys are in the set.
func (k Keys) Has(keys Keys) bool { return k&keys == keys }

// MousePress is the first valid press of a mouse button buffered for a tick.
// X and Y are logical viewport pixels measured from the top left. Read them
// only when Valid is true; a press at (0, 0) is valid.
type MousePress struct {
	X, Y  float64
	Valid bool
}

// Pointer is the mouse position sampled for a tick. X and Y are logical
// viewport pixels measured from the top left. Read them only when Inside is
// true; otherwise they are zero. Inside is false when the pointer is outside the
// viewport, including its letterbox bars, and when the window is not focused, so
// the position is unknown until the pointer next moves after focus returns.
type Pointer struct {
	X, Y   float64
	Inside bool
}

// Input is a value snapshot for one simulation tick. Held describes keys down
// at tick start, while Pressed includes new key presses since the previous tick.
// A short press can appear in Pressed without appearing in Held.
// PrimaryPress and SecondaryPress carry the left and right mouse-button edges,
// and PrimaryRelease and SecondaryRelease carry the matching release edges, at
// the last position the pointer had inside the viewport. Each edge is buffered
// independently and consumed by one tick, including during catch-up.
// PrimaryHeld and SecondaryHeld report the buttons that are down. A button
// press the host accepts in the viewport is followed by one release edge
// unless focus is lost first. Losing focus clears all edges and held state and
// emits no release, so a drag that ends without a release is cancelled.
type Input struct {
	Held             Keys
	Pressed          Keys
	Pointer          Pointer
	PrimaryPress     MousePress
	SecondaryPress   MousePress
	PrimaryRelease   MousePress
	SecondaryRelease MousePress
	PrimaryHeld      bool
	SecondaryHeld    bool
}

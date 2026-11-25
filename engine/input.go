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
//
// Touches lists the fingers on the screen in the order they landed, up to
// MaxTouches; a finger beyond the fifth is ignored. Active entries come first,
// and the entries after them are all zero. The first finger to land while no
// other finger is down drives the pointer fields exactly as the mouse does: its
// landing is a PrimaryPress, its movement moves the Pointer, PrimaryHeld is true
// while it is down, and its lifting is a PrimaryRelease. When it lifts while
// other fingers are down, the remaining fingers appear only in Touches until
// all have lifted. Touches is sampled like the Pointer, so a tap shorter than a
// tick can show its press and release edges without appearing in Touches.
// Losing focus clears every touch without a release. Desktop hosts report no
// touches, because GLFW has no touch input.
//
// Gamepads holds the pads in host slots 0 to MaxGamepads-1. A slot without a
// connected pad that has the standard layout is all zero. Pads are sampled
// once per presented frame, so the catch-up ticks of one frame see the same
// state. Hosts never turn a pad into keys or pointer input.
//
// Input holds only comparable values, so two snapshots compare with ==. See
// specs/runtime-m1.md for the host rules.
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
	Touches          [MaxTouches]Touch
	Gamepads         [MaxGamepads]Gamepad
}

const (
	// MaxTouches is the number of fingers an Input reports.
	MaxTouches = 5
	// MaxGamepads is the number of gamepad slots an Input reports.
	MaxGamepads = 4
)

// Touch is one finger on the screen. ID is nonzero and stays the same while
// the finger is down; the host numbers fingers in landing order and may reuse
// a number after every finger has lifted. X and Y are logical viewport pixels
// measured from the top left, the last position the finger had inside the
// viewport. An inactive Touch is all zero.
type Touch struct {
	ID     uint32
	X, Y   float64
	Active bool
}

// GamepadButtons is a bit set of gamepad buttons. Bit i is button i of the W3C
// Gamepad standard layout, so the names describe positions, not labels.
type GamepadButtons uint16

const (
	// GamepadSouth is the bottom face button, such as A or Cross.
	GamepadSouth GamepadButtons = 1 << iota
	// GamepadEast is the right face button, such as B or Circle.
	GamepadEast
	// GamepadWest is the left face button, such as X or Square.
	GamepadWest
	// GamepadNorth is the top face button, such as Y or Triangle.
	GamepadNorth
	GamepadLeftShoulder
	GamepadRightShoulder
	// GamepadLeftTrigger and GamepadRightTrigger are down while their trigger
	// value is at least 128.
	GamepadLeftTrigger
	GamepadRightTrigger
	GamepadBack
	GamepadStart
	// GamepadLeftStick and GamepadRightStick are the stick presses.
	GamepadLeftStick
	GamepadRightStick
	GamepadUp
	GamepadDown
	GamepadLeft
	GamepadRight
)

// Has reports whether all the specified buttons are in the set.
func (b GamepadButtons) Has(buttons GamepadButtons) bool { return b&buttons == buttons }

// Gamepad is the state of one pad with the standard layout. Stick axes run
// from -127 to 127, with left and up negative, after a circular deadzone of 15%
// whose remainder is rescaled to the full range. Triggers run from 0 to 255.
// Equal physical positions give equal values on every target. A disconnected
// slot is all zero.
type Gamepad struct {
	Connected                 bool
	Buttons                   GamepadButtons
	LeftX, LeftY              int8
	RightX, RightY            int8
	LeftTrigger, RightTrigger uint8
}

package loop

import (
	"math"

	"github.com/krapht-foundation/goje/engine"
)

// deadzone is the radius of the circular stick deadzone, as a fraction of a
// full deflection.
const deadzone = 0.15

// NormalizeGamepad converts the state a host reads from a standard-layout pad
// into an engine.Gamepad. Sticks are -1 to 1 with up negative, as both the W3C
// Gamepad API and GLFW report them, and triggers are 0 to 1. The trigger bits
// of buttons are ignored and derived from the trigger values, so a trigger
// counts as down at the same position on every target. Both hosts call this
// one function, and its arithmetic avoids fused operations, so equal positions
// give equal bytes on every target; see specs/runtime-m1.md.
func NormalizeGamepad(
	buttons engine.GamepadButtons,
	leftX, leftY, rightX, rightY, leftTrigger, rightTrigger float64,
) engine.Gamepad {
	pad := engine.Gamepad{
		Connected:    true,
		Buttons:      buttons &^ (engine.GamepadLeftTrigger | engine.GamepadRightTrigger),
		LeftTrigger:  trigger(leftTrigger),
		RightTrigger: trigger(rightTrigger),
	}
	pad.LeftX, pad.LeftY = stick(leftX, leftY)
	pad.RightX, pad.RightY = stick(rightX, rightY)
	if pad.LeftTrigger >= 128 {
		pad.Buttons |= engine.GamepadLeftTrigger
	}
	if pad.RightTrigger >= 128 {
		pad.Buttons |= engine.GamepadRightTrigger
	}
	return pad
}

// stick applies the circular deadzone and rescales the remaining radius, up to
// a full deflection, to the whole range. NaN and infinite input read as rest.
func stick(x, y float64) (stickX, stickY int8) {
	// The conversions stop the compiler from fusing the sum into a
	// fused multiply-add, which only some targets would round differently.
	r := math.Sqrt(float64(x*x) + float64(y*y))
	if !(r > deadzone) || math.IsInf(r, 0) {
		return 0, 0
	}
	scale := (min(r, 1) - deadzone) / ((1 - deadzone) * r)
	return axis(x * scale), axis(y * scale)
}

func axis(v float64) int8 { return int8(math.Round(min(max(v, -1), 1) * 127)) }

func trigger(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	return uint8(math.Round(min(v, 1) * 255))
}

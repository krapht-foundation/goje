//go:build cgo && (darwin || linux || windows)

package desktop

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/krapht-foundation/goje/engine"
)

// The states are shaped like those GLFW reports for a standard pad: triggers
// rest at -1, and sticks rest near but not at 0.
func TestGamepadFromGLFW(t *testing.T) {
	var resting [len(glfwButtons)]uint8
	pressed := resting
	pressed[glfw.ButtonA] = uint8(glfw.Press)
	pressed[glfw.ButtonLeftBumper] = uint8(glfw.Press)
	pressed[glfw.ButtonGuide] = uint8(glfw.Press)
	pressed[glfw.ButtonDpadUp] = uint8(glfw.Press)
	pressed[glfw.ButtonDpadLeft] = uint8(glfw.Press)
	for _, tc := range []struct {
		name    string
		buttons [len(glfwButtons)]uint8
		axes    [6]float32
		want    engine.Gamepad
	}{
		{
			"resting with drift", resting,
			[6]float32{0.03, -0.05, 0.1, 0, -1, -1},
			engine.Gamepad{Connected: true},
		},
		{
			"buttons", pressed,
			[6]float32{0, 0, 0, 0, -1, -1},
			engine.Gamepad{
				Connected: true,
				Buttons: engine.GamepadSouth | engine.GamepadLeftShoulder | engine.GamepadUp |
					engine.GamepadLeft,
			},
		},
		{
			"left stick up, right stick left, triggers pulled", resting,
			[6]float32{0, -1, -1, 0, 1, 0},
			engine.Gamepad{
				Connected: true,
				Buttons:   engine.GamepadLeftTrigger | engine.GamepadRightTrigger,
				LeftY:     -127, RightX: -127, LeftTrigger: 255, RightTrigger: 128,
			},
		},
	} {
		if got := gamepadFromGLFW(&tc.buttons, &tc.axes); got != tc.want {
			t.Errorf("%s: %+v; want %+v", tc.name, got, tc.want)
		}
	}
	axes := [6]float32{0.4, -0.2, 0, 0.9, -1, 0.5}
	if n := testing.AllocsPerRun(100, func() { gamepadFromGLFW(&pressed, &axes) }); n != 0 {
		t.Errorf("conversion allocates %v times", n)
	}
}

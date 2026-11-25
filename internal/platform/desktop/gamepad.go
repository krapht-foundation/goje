//go:build cgo && (darwin || linux)

package desktop

/*
// The GLFW headers are not on this package's include path, so the state type
// and function are declared here. The layout matches GLFWgamepadstate in GLFW
// 3.3, which the go-gl binding builds and links.
typedef struct { unsigned char buttons[15]; float axes[6]; } gojeGamepadState;
int glfwGetGamepadState(int jid, gojeGamepadState *state); // Provided by the linked GLFW library.
*/
import "C"

import (
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/loop"
)

// glfwButtons maps GLFW gamepad button indexes to the W3C standard layout bits.
// The guide button has no bit.
var glfwButtons = [...]engine.GamepadButtons{
	glfw.ButtonA:           engine.GamepadSouth,
	glfw.ButtonB:           engine.GamepadEast,
	glfw.ButtonX:           engine.GamepadWest,
	glfw.ButtonY:           engine.GamepadNorth,
	glfw.ButtonLeftBumper:  engine.GamepadLeftShoulder,
	glfw.ButtonRightBumper: engine.GamepadRightShoulder,
	glfw.ButtonBack:        engine.GamepadBack,
	glfw.ButtonStart:       engine.GamepadStart,
	glfw.ButtonGuide:       0,
	glfw.ButtonLeftThumb:   engine.GamepadLeftStick,
	glfw.ButtonRightThumb:  engine.GamepadRightStick,
	glfw.ButtonDpadUp:      engine.GamepadUp,
	glfw.ButtonDpadRight:   engine.GamepadRight,
	glfw.ButtonDpadDown:    engine.GamepadDown,
	glfw.ButtonDpadLeft:    engine.GamepadLeft,
}

// gamepads reads GLFW joysticks 1 to 4 into the matching engine slots. It
// reuses one state buffer, because the binding's GetGamepadState allocates a
// result on every call and a Go pointer passed to C escapes.
type gamepads struct{ state C.gojeGamepadState }

// poll runs on the main thread once per presented frame, before the updates.
func (g *gamepads) poll(l *loop.Loop) {
	for slot := range engine.MaxGamepads {
		var pad engine.Gamepad
		// GLFW reports false for a missing joystick and for one without a
		// standard mapping, which the engine reports as disconnected.
		if C.glfwGetGamepadState(C.int(glfw.Joystick1)+C.int(slot), &g.state) != 0 {
			var buttons [len(glfwButtons)]uint8
			var axes [6]float32
			for i := range buttons {
				buttons[i] = uint8(g.state.buttons[i])
			}
			for i := range axes {
				axes[i] = float32(g.state.axes[i])
			}
			pad = gamepadFromGLFW(&buttons, &axes)
		}
		l.SetGamepad(slot, pad)
	}
}

// gamepadFromGLFW converts a GLFW gamepad state. GLFW reports triggers from -1
// at rest to 1, and the engine from 0 to 1.
func gamepadFromGLFW(buttons *[len(glfwButtons)]uint8, axes *[6]float32) engine.Gamepad {
	var pressed engine.GamepadButtons
	for i, bit := range glfwButtons {
		if glfw.Action(buttons[i]) == glfw.Press {
			pressed |= bit
		}
	}
	return loop.NormalizeGamepad(pressed,
		float64(axes[glfw.AxisLeftX]), float64(axes[glfw.AxisLeftY]),
		float64(axes[glfw.AxisRightX]), float64(axes[glfw.AxisRightY]),
		(float64(axes[glfw.AxisLeftTrigger])+1)/2, (float64(axes[glfw.AxisRightTrigger])+1)/2)
}

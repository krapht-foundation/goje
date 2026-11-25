//go:build cgo && (darwin || linux || windows)

package desktop

// int glfwGetError(const char **description); // Provided by the linked GLFW library.
import "C"

import (
	"errors"
	"fmt"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func initGLFW() error {
	// Clear any native error from an earlier run on this thread.
	C.glfwGetError(nil)
	err := glfw.Init()
	if err != nil {
		return fmt.Errorf("initialize GLFW: %w", err)
	}
	err = nativeGLFWError()
	if err != nil {
		glfw.Terminate()
		return fmt.Errorf("initialize GLFW: %w", err)
	}
	return nil
}

// The Go binding logs GLFW_PLATFORM_ERROR and returns nil. The native error
// remains available on the calling thread, including when X11 cannot open a
// display. Recover it during startup so the caller receives a launch error.
func nativeGLFWError() error {
	var description *C.char
	//nolint:gocritic // dupSubExpr flags equality in the generated cgo wrapper.
	code := int(C.glfwGetError(&description))
	if code == 0 {
		return nil
	}
	return fmt.Errorf("%s (GLFW error %#x)", C.GoString(description), code)
}

func glfwFailure() error {
	if err := nativeGLFWError(); err != nil {
		return err
	}
	return errors.New("GLFW did not return a window")
}

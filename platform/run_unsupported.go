//go:build !(js && wasm) && !(cgo && (darwin || linux || windows))

package platform

import (
	"context"
	"errors"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

func run(context.Context, driver.Config, engine.SceneFactory) error {
	return errors.New(
		"Goje graphics require macOS, Linux, or Windows with CGO, or a js/wasm build with WebGL 2",
	)
}

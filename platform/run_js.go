//go:build wasm

package platform

import (
	"context"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/browser"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

func run(ctx context.Context, config driver.Config, scene engine.SceneFactory) error {
	return browser.Run(ctx, config, scene)
}

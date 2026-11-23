//go:build cgo && (darwin || linux)

package platform

import (
	"context"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

func run(ctx context.Context, config driver.Config, scene engine.SceneFactory) error {
	return desktop.Run(ctx, config, scene)
}

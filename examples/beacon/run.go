//go:build !goje_beacon_smoke && !goje_bench

package main

import (
	"context"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/platform"
)

func run(config platform.Config, factory func() *scene) error {
	return platform.Run(context.Background(), config, func() engine.Scene { return factory() })
}

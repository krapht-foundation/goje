//go:build goje_bench && !goje_beacon_smoke

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
	"github.com/krapht-foundation/goje/platform"
)

func run(config platform.Config, factory func() *scene) error {
	mode := os.Getenv("GOJE_BENCH_MODE")
	switch mode {
	case "off":
		config.Sounds, config.Music = nil, nil
	case "idle", "burst":
	default:
		return fmt.Errorf("unknown Beacon audio benchmark mode %q", mode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	sampler := driver.StartBenchmark(cancel, true)
	if err := platform.Run(ctx, config, func() engine.Scene {
		return &benchScene{Scene: factory(), burst: mode == "burst"}
	}); err != nil {
		return fmt.Errorf("run Beacon audio benchmark: %w", err)
	}
	samples, err := sampler.Report()
	if err != nil {
		return err
	}
	report := struct {
		Benchmark string `json:"benchmark"`
		Mode      string `json:"mode"`
		Fixture   string `json:"fixture"`
		driver.BenchmarkReport
	}{Benchmark: "goje", Mode: mode, Fixture: "beacon", BenchmarkReport: samples}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return fmt.Errorf("write Beacon audio benchmark: %w", err)
	}
	return nil
}

type benchScene struct {
	engine.Scene
	burst bool
}

func (s *benchScene) Update(tick engine.Tick) (engine.Transition, error) {
	// Twenty cues per simulation second overlap the 120 ms reference cue.
	// They exercise cancellation and replacement without altering host pacing.
	// Requested directly because a beacon lights only once per round.
	if s.burst && tick.Number%3 == 0 {
		tick.PlaySound(toggleCue)
	}
	return s.Scene.Update(tick)
}

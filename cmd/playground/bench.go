//go:build goje_bench && !goje_smoke

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
	"github.com/krapht-foundation/goje/platform"
)

const (
	benchSprites       = 2048
	benchTraceCapacity = 2048
	benchTimeout       = 60 * time.Second
)

// benchReport puts the report marker first so the browser runner can recognize
// the JSON line among other messages emitted by the Wasm console adapter.
type benchReport struct {
	Benchmark  string `json:"benchmark"`
	Mode       string `json:"mode"`
	Sprites    int    `json:"sprites"`
	Fixture    string `json:"fixture"`
	WorldItems int    `json:"world_items,omitempty"`
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	driver.BenchmarkReport
	TraceCapacity          int    `json:"trace_capacity"`
	TraceRecords           int    `json:"trace_records"`
	OmittedRecords         uint64 `json:"omitted_records"`
	DiagnosticTicks        uint64 `json:"diagnostic_ticks"`
	DiagnosticFrames       uint64 `json:"diagnostic_frames"`
	DiagnosticDroppedTicks uint64 `json:"diagnostic_dropped_ticks"`
}

// benchScene uses the real encounter, with scripted input and an optional grid
// of textured quads that stresses the same batched renderer and shared texture.
type benchScene struct {
	engine.Scene
	sprites int
}

func main() {
	if err := runBenchmark(); err != nil {
		fmt.Fprintf(os.Stderr, "GOJE_BENCH_ERROR %v\n", err)
		os.Exit(1)
	}
}

func runBenchmark() error {
	mode := os.Getenv("GOJE_BENCH_MODE")
	if mode != "off" && mode != "on" {
		return errors.New("GOJE_BENCH_MODE must be off or on")
	}
	sprites, err := strconv.Atoi(os.Getenv("GOJE_BENCH_SPRITES"))
	if err != nil || (sprites != 0 && sprites != benchSprites) {
		return fmt.Errorf("GOJE_BENCH_SPRITES must be 0 or %d", benchSprites)
	}
	fixture := os.Getenv("GOJE_BENCH_FIXTURE")
	if fixture == "" {
		fixture = "pickup"
	}
	var factory engine.SceneFactory
	var worldItems int
	switch fixture {
	case "pickup":
		factory = func() engine.Scene { return &benchScene{Scene: newScene(), sprites: sprites} }
	case "courtyard":
		worldItems = 3
		if sprites != 0 {
			worldItems = sprites
		}
		factory = func() engine.Scene { return newBenchmarkCourtyard(worldItems) }
	default:
		return fmt.Errorf("unknown benchmark fixture: %q", fixture)
	}

	// The sampler cancels on completion. The deadline bounds runs stalled by a
	// suspended window or tab, rather than producing incomplete timing data.
	ctx, cancel := context.WithTimeout(context.Background(), benchTimeout)
	defer cancel()
	sampler := driver.StartBenchmark(cancel)
	config := playgroundConfig()
	if mode == "on" {
		config.Diagnostics = &platform.Diagnostics{TraceCapacity: benchTraceCapacity}
	}
	if runErr := platform.Run(ctx, config, factory); runErr != nil {
		return fmt.Errorf("run playground benchmark: %w", runErr)
	}

	samples, err := sampler.Report()
	if err != nil {
		return err
	}
	report := benchReport{
		Benchmark: "goje", Mode: mode, Sprites: sprites,
		Fixture: fixture, WorldItems: worldItems,
		GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		BenchmarkReport: samples,
	}
	if d := config.Diagnostics; d != nil {
		report.TraceCapacity = d.TraceCapacity
		report.TraceRecords = len(d.Result.Trace)
		report.OmittedRecords = d.Result.OmittedRecords
		report.DiagnosticTicks = d.Result.Ticks
		report.DiagnosticFrames = d.Result.Frames
		report.DiagnosticDroppedTicks = d.Result.DroppedTicks
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return fmt.Errorf("write benchmark report: %w", err)
	}
	return nil
}

func (s *benchScene) Update(tick engine.Tick) (engine.Transition, error) {
	// Apply the same patrol on every run without depending on OS input or frame
	// pacing. This path moves the player but never changes the token state.
	patrol := [...]engine.Keys{
		engine.KeyArrowLeft, engine.KeyArrowUp,
		engine.KeyArrowRight, engine.KeyArrowDown,
	}
	tick.Input = engine.Input{Held: patrol[(tick.Number-1)/16%uint64(len(patrol))]}
	return s.Scene.Update(tick)
}

func (s *benchScene) Draw(canvas engine.Canvas) {
	s.Scene.Draw(canvas)
	const columns = 64
	for i := range s.sprites {
		canvas.Image(float32(i%columns*5), float32(i/columns*6), 4, 4)
	}
}

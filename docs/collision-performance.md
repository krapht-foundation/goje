# Measure collision

The `collidebench` program runs the [collision package](../engine/collide/collide.go) on a dense, repeatable scene and prints one JSON report. The board is 64 × 64 tiles with 680 one-tile solid pillars (about one tile in six) and 64 two-tile trigger zones. The scene places 2,048 bodies of 3/8 tile in free tiles. Each tick moves every body by its own fixed velocity of up to 24/256 tile per axis, reverses an axis when a body is stopped, and then asks the trigger tracker for enter and exit events. A body never overlaps a pillar: `Move` would report an error if it did, and the run would fail.

The same program runs natively and as WebAssembly in Chromium, so the reports are comparable. It does not draw anything, read input, or use the browser's rendering loop; it measures the collision rules alone. The cost in a real scene also includes drawing and game rules.

```sh
make setup-web
make benchmark-collide
```

Each run warms up for 60 ticks and measures 600. The report gives median, 95th, and 99th percentile microseconds for the movement phase, the trigger phase, and their sum, and the number of Go heap allocations during the measured ticks. `BENCHMARK_REPEATS` sets the number of runs per target (default 3). The hash covers every body position and every trigger event, so equal hashes mean the targets simulated the same history.

## Results

Measured on an Apple M4 Pro with macOS 15.5, Go 1.27.4, and headless Chromium 159.0.8154.6, with five runs per target. The table shows the median of each run's percentile.

| Target | Move median / p95 / p99 (µs) | Triggers median / p95 / p99 (µs) | Tick median / p95 / p99 (µs) | Allocations |
| --- | ---: | ---: | ---: | ---: |
| Native arm64 | 74.8 / 82.9 / 89.5 | 26.5 / 30.1 / 34.2 | 101.8 / 111.1 / 120.5 | 0 |
| Chromium WebAssembly | 299.8 / 300.3 / 400.1 | 99.8 / 199.9 / 200.2 | 300.0 / 400.1 / 500.0 | 0 |

All ten runs reported zero allocations and the same hash, `2488111921216595907`, so native and Chromium produced identical positions and events for the 660 simulated ticks. Chromium's clock advances in steps of about 100 µs, so its values are rounded and its percentiles move in steps. A direct run of the same program under Node's WebAssembly runner, which has a finer clock, measured a 368 µs median tick and produced the same hash. That suggests the true Chromium cost is somewhat above the rounded 300 µs, about 3.6 times the native median.

At 60 ticks per second the fixed step is 16.7 ms. The measured scene uses well under 3% of that on either target. This is a single dense scene on one machine; it does not predict other layouts, such as a few very large solids, and it measures neither Linux nor other browsers.

The unit tests also confirm the index against a scan of every shape, and `Move` against a unit-step reference, on randomized small worlds. A warm tick allocated nothing under `testing.AllocsPerRun`.

# Measure ground tile rendering

The `ground` playground benchmark compares the same scene with tile quads off and on. Both modes load the same atlas, use the same camera and 320 × 200 logical viewport, keep the grid and entities, and disable diagnostics. The small workload is the authored 9 × 7 courtyard. The dense workload has 16,384 walkable cells and deliberately submits off-screen ground tiles to measure rendering without culling. Neither workload sends input.

```sh
make setup-web
make BENCHMARK_FIXTURE=ground benchmark-desktop benchmark-web
```

Each graphical run warms up for 30 frames and samples 150. Three paired repetitions alternate run order by default. The report records frame CPU time through submission, frame-start intervals, dropped ticks, and peak vertices and bytes. The upload field times the texture submission call, including the JavaScript buffer copy on web. It does not wait for GPU completion. Run the targets sequentially so the hosts do not compete for resources.

Use the headless benchmarks to measure PNG decode, atlas packing, and allocations after buffer warmup. They use the same drawing path but omit graphics submission. The Wasm command runs under Node; it does not measure browser or GPU timing.

```sh
GOMAXPROCS=1 go test -tags goje_bench ./cmd/playground -run '^$' \
  -bench '^BenchmarkGround' -benchmem -benchtime=100ms -count=10
GOMAXPROCS=1 GOOS=js GOARCH=wasm go test -tags goje_bench \
  -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./cmd/playground -run '^$' \
  -bench '^BenchmarkGround' -benchmem -benchtime=100ms -count=10
```

## Results

Measured on an Apple M4 Pro with macOS 15.5, Go 1.27.4, Node 24.20.0, and headless Chromium 159.0.8154.6. The native host used its default 640 × 400 window on a Retina display; Chromium used 640 × 400 CSS pixels at DPR 1. Compare modes within each host rather than comparing the hosts directly.

Median of three graphical runs per case. Frame times are CPU time through submission; interval p95 is measured between starts of consecutive frames, not GPU time.

| Target | Board | Tile art | Vertices / bytes per frame | Frame median / p95 / p99 (µs) | Interval p95 (ms) |
| --- | --- | --- | ---: | ---: | ---: |
| Native | Courtyard | Off | 138 / 2,760 | 39 / 187 / 398 | 12.40 |
| Native | Courtyard | On | 498 / 9,960 | 41 / 117 / 396 | 12.37 |
| Native | Dense | Off | 1,560 / 31,200 | 46 / 163 / 460 | 12.38 |
| Native | Dense | On | 99,864 / 1,997,280 | 402 / 527 / 572 | 12.49 |
| Chromium | Courtyard | Off | 138 / 2,760 | 100 / 200 / 200 | 17.30 |
| Chromium | Courtyard | On | 498 / 9,960 | 100 / 100 / 200 | 17.24 |
| Chromium | Dense | Off | 1,560 / 31,200 | 100 / 200 / 200 | 17.50 |
| Chromium | Dense | On | 99,864 / 1,997,280 | 1,200 / 1,300 / 1,400 | 17.50 |

No run dropped a simulation tick. The browser timer's observed step was about 100 µs; the courtyard results do not establish a timing gain or regression. The dense workload does show additional CPU work and nearly 2 MB of submitted data per frame, most of it outside the viewport. Culling is not required for this milestone's courtyard. Measure viewport culling before adopting this draw path for larger, busier maps.

All ten warmed headless repetitions per case reported 0 B/op and 0 allocations/op. Median time to tick and record a frame was 0.47 → 1.31 µs native and 2.07 → 6.03 µs under Node Wasm for the courtyard, and 5.01 → 283 µs native and 21.7 µs → 4.32 ms under Node Wasm for the dense board. The Node and Chromium results come from different hosts and timing methods.

| Startup stage | Native | Wasm |
| --- | ---: | ---: |
| Decode the 217-byte reference PNG | 4.29 µs | 13.57 µs (Node) |
| Pack the tile and marker into one 45 × 16 atlas | 0.73 µs | 3.95 µs (Node) |
| Submit the atlas upload | 80.69 µs | ~100 µs (Chromium) |

Decode and pack are medians of ten headless runs. Upload is the median of twelve graphical runs with the same atlas; one Chromium sample was below the timer's resolution.

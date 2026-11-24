# Measure sprite animation

The `sprite` benchmark compares a fixed frame with the tomato's looping movement clip. It uses the authored courtyard's ground and camera with a 320 × 200 logical viewport. The small workload places one actor at the scout's spawn. The dense workload places 2,048 visible, overlapping actors with separate players. Actors stay at the same ground anchors and have the same draw order in both modes. There is no movement, picking, or pathfinding in either mode. The animated pose can change the covered pixels, but both modes draw exactly one textured quad per actor from the same atlas.

```sh
make setup-web
make BENCHMARK_FIXTURE=sprite benchmark-desktop benchmark-web
```

Run the targets sequentially. Each mode warms up for 30 frames and measures 150, with three paired repetitions in alternating order by default. `off` holds the first frame; `on` advances the clip once per simulation tick. Diagnostics are disabled. The host reports tick and CPU frame durations, frame-start intervals, dropped ticks, submitted vertices and bytes, and Go heap allocation totals over the sample window. The allocation totals cover the Go process, including host work, but not JavaScript or GPU allocations. The upload time ends at submission, not GPU completion.

To isolate allocation and startup cost from graphics submission, run the same batch recording headlessly. The Wasm runner uses Node, not Chromium.

```sh
GOMAXPROCS=1 go test -tags goje_bench ./cmd/playground -run '^TestSpriteBenchmark' \
  -bench '^BenchmarkSprite' -benchmem -benchtime=100ms -count=10
GOMAXPROCS=1 GOOS=js GOARCH=wasm go test -tags goje_bench \
  -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./cmd/playground -run '^TestSpriteBenchmark' \
  -bench '^BenchmarkSprite' -benchmem -benchtime=100ms -count=10
```

## Results

Measured on an Apple M4 Pro with macOS 15.5, Go 1.27.4, Node 24.20.0, and headless Chromium 159.0.8154.6. Compare modes within a host. The native window uses Retina scaling; headless Chromium uses 640 × 400 CSS pixels at DPR 1.

### Graphical hosts

Each row shows medians from three runs. Zero-duration Chromium samples fell below the observed timer step of roughly 100 µs; they do not mean the work was free.

| Host | Actors | Frame choice | Tick median / p95 / p99 (µs) | CPU frame median / p95 / p99 (µs) | Frame interval p95 (ms) |
| --- | ---: | --- | ---: | ---: | ---: |
| OpenGL | 1 | Fixed | 0.58 / 2.13 / 71.00 | 56 / 222 / 408 | 16.71 |
| OpenGL | 1 | Animated | 0.67 / 1.92 / 10.96 | 48 / 115 / 238 | 12.25 |
| OpenGL | 2,048 | Fixed | 0.46 / 1.25 / 25.42 | 124 / 190 / 254 | 12.36 |
| OpenGL | 2,048 | Animated | 4.04 / 29.08 / 44.29 | 120 / 220 / 270 | 12.35 |
| Chromium | 1 | Fixed | 0 / 0 / 100 | 0 / 100 / 200 | 17.40 |
| Chromium | 1 | Animated | 0 / 0 / 100 | 100 / 100 / 200 | 17.50 |
| Chromium | 2,048 | Fixed | 0 / 0 / 100 | 200 / 300 / 300 | 17.50 |
| Chromium | 2,048 | Animated | 0 / 100 / 100 | 200 / 300 / 300 | 17.50 |

All 24 graphical runs reported zero dropped ticks and zero sample overflows. Fixed and animated modes each submitted 474 vertices (9,480 B) per frame with one actor and 12,756 vertices (255,120 B) with 2,048 actors. The dense native workload spent more CPU time advancing ticks; frame interval p95 did not increase at that density on either host. The one-actor timing spread and the Chromium clock resolution do not establish a small-scene speedup or regression.

Every Chromium run recorded 596 Go heap allocations (7,152 B) per sample window, identical across modes and densities. Native recorded zero allocations in all animated runs and all but one fixed run. That fixed dense run recorded 7 allocations (5,576 B) in its process-wide window. The one-time atlas upload submission took a median of 67.6 µs on native and about 100 µs in Chromium.

### Headless

All ten warmed repetitions per case reported **0 B/op and 0 allocations/op**. Times below are medians. Each advance runs a fixed tick and records a render batch, without submitting it to graphics.

| Workload | Native fixed → animated | Node Wasm fixed → animated |
| --- | ---: | ---: |
| 1 actor | 1.23 → 1.24 µs | 5.74 → 5.86 µs |
| 2,048 actors | 36.1 → 44.3 µs | 153 → 174 µs |

The one-actor ranges overlap. For the dense workload, the animated batch added about 8 µs on native and 21 µs on Node Wasm; no measured fixed/animated ranges overlapped on either target.

| Startup stage | Native | Node Wasm | Go allocations per operation |
| --- | ---: | ---: | ---: |
| Decode and validate the 485-byte PNG and 636-byte clip sidecar | 21.36 µs | 66.94 µs | 189 (64,336 B) |
| Pack the decoded sheet and ground tile into the 144 × 24 atlas | 0.80 µs | 1.85 µs | 2 (14,400 B) |

Packing measures pixel composition only; it excludes decode, frame-coordinate rebasing, and texture upload.

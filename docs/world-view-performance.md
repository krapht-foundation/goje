# M2 world-view performance

The benchmark compares the reference courtyard with a dense version of the same scene, then reruns the unchanged pickup fixture against the [M1 baseline](runtime-baseline.md). It measures CPU-side work, not GPU completion or input latency.

## Reproduce the runs

Use a supported desktop with a visible window and a quiet machine. Run one measurement at a time. Run `make setup-web` once for the pinned Chromium and Playwright dependencies.

```sh
mkdir -p .build
make -s benchmark-desktop > .build/m2-pickup-desktop.jsonl
make -s benchmark-desktop BENCHMARK_FIXTURE=courtyard > .build/m2-courtyard-desktop.jsonl
make -s benchmark-web > .build/m2-pickup-web.jsonl
make -s benchmark-web BENCHMARK_FIXTURE=courtyard > .build/m2-courtyard-web.jsonl
```

The courtyard starts its patrol at tick one and attempts a pick every eight ticks, alternating a point near the wall with a guaranteed miss. The dense run has 2,048 total world items: the two patrolling units, one wall, and 2,045 static selectable sprites. Each tick sorts the dense list after moving its units; picking and drawing use this same list. The synthetic sprites are in projected view and use the shared image. The M1 dense fixture instead adds 2,048 already-ordered screen quads with no extra world state or picking. It is not an equivalent density workload.

Both fixture suites warm up for 30 frames and sample 150, with three off/on pairs per density by default. Set `BENCHMARK_REPEATS` to change the paired run count. The same sampler runs in both capture modes; `world_items` labels the actual list size for courtyard reports. Each report records the tick, CPU frame, and frame interval percentiles and the number of dropped ticks. See the [M1 measurement definitions](runtime-baseline.md#reproduce-the-comparison) for the sample boundaries.

The headless measurements isolate ordering and a full missed query in reusable buffers. `BenchmarkCourtyardAdvance` also includes a fixed tick and the real reusable draw batch, without the windowing or WebGL calls. Its warmup runs a whole patrol so batch growth does not appear as a steady-state allocation.

```sh
GOMAXPROCS=1 go test -tags goje_bench ./engine ./cmd/playground \
  -run '^$' -bench '^(BenchmarkWorldView|BenchmarkCourtyardAdvance)$' -benchmem -benchtime=100ms -count=10
GOMAXPROCS=1 GOOS=js GOARCH=wasm go test -tags goje_bench \
  -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./engine ./cmd/playground \
  -run '^$' -bench '^(BenchmarkWorldView|BenchmarkCourtyardAdvance)$' -benchmem -benchtime=100ms -count=10
```

## Graphical results

The runs used Go 1.27.4 on an Apple M4 Pro with 24 GiB of memory and macOS 15.5 (24F74). Native runs used CGO, GLFW/OpenGL, and a 640×400 window on the built-in Retina display. Browser runs used Node 24.20.0 to drive Playwright 1.65.1 and headless Chromium 159.0.8154.6 at 640×400 and DPR 1 with default renderer flags. Both used a 320×200 logical viewport.

Each timing cell gives the median of three per-run p50 / p95 / p99 values. No ticks dropped and no tick samples overflowed across the 48 runs.

| Host | Courtyard items | Capture | Tick (µs) | CPU frame (ms) | Frame interval (ms) |
| --- | ---: | --- | --- | --- | --- |
| macOS | 3 | Off | 1.67 / 39.54 / 95.21 | 0.088 / 0.222 / 0.393 | 8.376 / 16.859 / 17.281 |
| macOS | 3 | On | 2.00 / 51.25 / 127.25 | 0.076 / 0.222 / 0.329 | 8.522 / 16.781 / 17.049 |
| macOS | 2,048 | Off | 29.42 / 88.50 / 117.58 | 0.117 / 0.221 / 0.365 | 8.342 / 16.770 / 16.902 |
| macOS | 2,048 | On | 36.79 / 80.54 / 99.25 | 0.131 / 0.244 / 0.362 | 8.346 / 16.787 / 18.458 |
| Chromium | 3 | Off | 0 / 100 / 100 | 0.100 / 0.200 / 0.300 | 16.700 / 17.500 / 17.700 |
| Chromium | 3 | On | 0 / 100 / 100 | 0.100 / 0.200 / 0.300 | 16.700 / 17.500 / 17.600 |
| Chromium | 2,048 | Off | 0 / 100 / 100 | 0.300 / 0.400 / 0.500 | 16.700 / 17.400 / 17.600 |
| Chromium | 2,048 | On | 0 / 100 / 200 | 0.300 / 0.400 / 0.500 | 16.700 / 17.500 / 17.600 |

The dense scene increased median native update cost by about 28 µs with capture off. Chromium's clock advances in roughly 100 µs steps: 55–64% of dense ticks rounded to zero. Use the isolated headless measurements below to inspect smaller costs instead of reading zero as a free update. Native frame timing varied across runs; the lower dense CPU-frame p99 with capture off does not imply that adding items speeds up presentation.

## Unchanged pickup compared with M1

The original movement script, sprite counts, window sizes, sample windows, and capture settings were unchanged. These are comparisons between separate measurement sessions, not randomized same-session before/after tests.

| Host | Pickup workload | Capture | CPU-frame p95 M1 → M2 (ms) | Frame-interval p95 M1 → M2 (ms) |
| --- | --- | --- | --- | --- |
| macOS | Reference | Off | 0.118 → 0.118 | 16.645 → 16.730 |
| macOS | Reference | On | 0.144 → 0.122 | 16.971 → 16.858 |
| macOS | +2,048 quads | Off | 0.335 → 0.357 | 17.442 → 16.923 |
| macOS | +2,048 quads | On | 0.172 → 0.225 | 16.862 → 16.844 |
| Chromium | Reference | Off | 0.200 → 0.200 | 17.400 → 17.400 |
| Chromium | Reference | On | 0.200 → 0.200 | 17.500 → 17.500 |
| Chromium | +2,048 quads | Off | 0.300 → 0.300 | 17.500 → 17.500 |
| Chromium | +2,048 quads | On | 0.300 → 0.300 | 17.500 → 17.500 |

Frame pacing did not show a consistent slowdown and neither session dropped ticks. The native dense capture-on CPU-frame p95 increased by 53 µs, but its per-run ranges overlapped (164–351 µs in M1 and 171–255 µs in M2). The reference capture-off native CPU-frame p99 increased from 0.171 ms to 0.232 ms, and Chromium's reference p99 moved from 0.200 ms to 0.300 ms in both capture modes (one clock step). These short runs do not rule out smaller or intermittent regressions.

## Headless results and allocations

Each value is the median of ten runs with `GOMAXPROCS=1`. Wasm ran under Node 24.20.0, not Chromium. The isolated ordering benchmark swaps the ground coordinates of the first and last item before each sort; the courtyard patrol changes nearby depths, so its full advance can take less time than this synthetic reorder.

| Workload | Items | Native (µs/op) | Wasm in Node (µs/op) |
| --- | ---: | ---: | ---: |
| Reorder after swapping ground positions | 3 | 0.029 | 0.102 |
| Full missed hit query | 3 | 0.003 | 0.009 |
| Courtyard tick and record draw batch | 3 | 0.548 | 2.532 |
| Reorder after swapping ground positions | 2,048 | 50.675 | 147.792 |
| Full missed hit query | 2,048 | 2.181 | 2.653 |
| Courtyard tick and record draw batch | 2,048 | 43.611 | 197.630 |

Every headless run reported **0 B/op and 0 allocs/op** once buffers were warm. That result does not include GLFW, browser event handling, JavaScript glue, or GPU submission. With frame intervals unchanged within the noise of these samples and no dropped ticks, this workload does not justify a spatial index or a parallel sort.

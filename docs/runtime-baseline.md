# M1 runtime performance baseline

This baseline measures what enabling local counters and bounded phase capture costs. It does not measure GPU completion or certify performance on other machines. The engine's diagnostics format and developer API are described in the [runtime guide](runtime.md#inspect-a-local-run).

## Reproduce the comparison

Use a supported graphical desktop for the native run. Keep the benchmark window visible, avoid resizing it, and do not run the desktop and browser measurements concurrently. The native benchmark build ignores focus changes so the scripted workload can finish; ordinary builds still suspend on focus loss. Use a quiet system and compare paired runs on the same hardware and build. Run `make setup-web` once to install the pinned Playwright package and Chromium.

```sh
mkdir -p .build
GOMAXPROCS=1 go test ./internal/platform/driver -run '^$' -bench '^BenchmarkAdvanceDiagnosticsDisabled$' -benchmem -count=10
make -s benchmark-desktop > .build/baseline-desktop.jsonl
make -s benchmark-web > .build/baseline-web.jsonl
```

Each graphical target builds one `goje_bench` artifact and uses it for both modes. The same sampler runs in both modes, independently of the diagnostic clock. The game uses scripted input in the reference encounter. The second workload adds 2,048 textured quads per frame using the same image and batch. Each run warms up for 30 frames and then samples 150; the default is three paired repeats per workload, alternating which mode runs first. Set `BENCHMARK_REPEATS` to increase that count. The enabled mode reserves 2,048 trace records and does not supply a logger. Benchmark-only probes compile out of ordinary builds.

- **Tick:** elapsed time for one fixed host update, from taking an input snapshot through the runtime step and any enabled diagnostics. An update can fall below the clock's observable resolution.
- **Frame:** elapsed CPU-side work from the host frame boundary through graphics submission. This includes fixed updates, draw recording, diagnostic work inside those phases, and time blocked in graphics calls. It excludes native buffer swap and browser paint.
- **Interval:** wall time between consecutive frame starts, including pacing or swap waits. Larger tails can reveal pacing regressions that frame work alone misses.

Each JSON line contains the nearest-rank p50, p95, and p99 for tick, frame, and interval; sample counts; dropped ticks in the measured window; clock granularity; and capture capacity and omissions. Durations are in nanoseconds. The clock probe runs after shutdown, so the probe itself does not affect the samples.

## Measured results

The runs used Go 1.27.4 on an Apple M4 Pro with 24 GiB of memory and macOS 15.5 (24F74). The native build used `darwin/arm64`, CGO, and GLFW/OpenGL with a 640×400 window on the built-in Retina display. The browser build used `js/wasm` in Playwright 1.65.1 with headless Chromium 159.0.8154.6, a 640×400 viewport at DPR 1, and default renderer flags. Both used a 320×200 logical game viewport.

The headless driver benchmark ran a no-op scene with one fixed tick and one recorded draw per operation. With diagnostics disabled, all ten runs reported **0 B/op and 0 allocs/op**, at 9.10–9.87 ns/op.

Each timing cell below is the median of the three runs' p50 / p95 / p99 values, not a percentile pooled across runs. `Reference` is the playground alone; `Dense` adds 2,048 quads.

| Host | Workload | Capture | Tick (µs) | CPU frame (ms) | Frame interval (ms) | Dropped ticks |
| --- | --- | --- | --- | --- | --- | ---: |
| macOS | Reference | Off | 0.71 / 22.42 / 37.17 | 0.056 / 0.118 / 0.171 | 8.43 / 16.64 / 21.05 | 0 |
| macOS | Reference | On | 0.96 / 3.17 / 42.71 | 0.061 / 0.144 / 0.190 | 8.38 / 16.97 / 18.96 | 0 |
| macOS | Dense | Off | 1.00 / 30.92 / 48.96 | 0.198 / 0.335 / 0.452 | 16.54 / 17.44 / 18.39 | 0 |
| macOS | Dense | On | 0.83 / 4.17 / 65.46 | 0.108 / 0.172 / 0.225 | 8.47 / 16.86 / 17.01 | 0 |
| Chromium | Reference | Off | 0 / 100 / 100 | 0.100 / 0.200 / 0.200 | 16.70 / 17.40 / 17.70 | 0 |
| Chromium | Reference | On | 0 / 100 / 100 | 0.100 / 0.200 / 0.200 | 16.70 / 17.50 / 17.70 | 0 |
| Chromium | Dense | Off | 0 / 0 / 100 | 0.200 / 0.300 / 0.400 | 16.70 / 17.50 / 17.70 | 0 |
| Chromium | Dense | On | 0 / 100 / 100 | 0.200 / 0.300 / 0.400 | 16.70 / 17.50 / 17.70 | 0 |

All 24 runs completed the sample window without dropping a tick. Enabled runs reserved 2,048 records each, retained 451–540 including warmup, and omitted none. Controlled tests exercise overflow separately.

No repeatable frame-pacing regression appeared in these runs. The macOS reference CPU-frame p95 differences within pairs were −34, +5, and +37 µs; dense differences were −16, +16, and −255 µs. Frame-interval p95 also changed direction across pairs. Native frame intervals varied substantially between runs, so the lower dense median with capture on is not evidence that capture improves performance. In Chromium, frame-time quantiles changed by at most one observable clock step (about 100 µs) across pairs, and 90–97% of tick samples rounded to zero. This measurement cannot resolve small browser update overhead. The results establish an initial comparison, not a promise that enabled capture is free.

---
name: goje-performance
description: Profile or optimize Goje simulation, rendering, memory, asset processing, or browser startup; design repeatable benchmarks and review performance claims.
---

# Measure, change, verify

1. State the workload, target, and metric first. Include representative scene density and a stress case. Separate startup or asset loading from steady state. Use frame or tick duration percentiles for stalls; averages and FPS alone hide spikes. For real-time workloads also track missed deadlines when relevant.
2. Record Go version, build flags, target, machine or browser, scene settings, and data size. Use the same setup for before and after. Distinguish CPU simulation, allocations and GC, submission, GPU work, and platform boundary cost. Use `pprof` or `go tool trace` where supported and browser performance tooling for the web build.
3. Fix the measured constraint first: remove unnecessary work, reduce repeated scans or boundary crossings, and only then tune representation. Keep frame/tick paths free of avoidable allocation or copying. Use bounded reuse with explicit ownership; a pool, unsafe code, goroutines, or SIMD needs target-specific evidence and must not weaken correctness.
4. Use focused `go test -run '^$' -bench '<name>' -benchmem -count=10 ./path/to/package` runs and compare outputs with `benchstat`. In new benchmarks use `b.Loop()` and keep unrelated fixture setup out of timing. Pair microbenchmarks with a representative playground workload; report inputs, before/after figures, and tradeoffs. Do not claim a win below run-to-run noise.

//go:build goje_bench

package driver

import (
	"context"
	"fmt"
	"slices"
	"time"
)

const (
	benchmarkWarmupFrames   = 30
	benchmarkMeasuredFrames = 150
	benchmarkTotalFrames    = benchmarkWarmupFrames + benchmarkMeasuredFrames
	// The M1 scheduler allows at most four updates per frame.
	benchmarkTickSamples = 4 * benchmarkMeasuredFrames
	benchmarkClockReads  = 20_000
)

// Benchmark owns the fixed sample buffers for one playground benchmark run.
// The host loop owns all writes; summarize the samples only after Run returns.
type Benchmark struct {
	cancel       context.CancelFunc
	lastStart    time.Time
	framesSeen   int
	tickCount    int
	tickOverflow int
	dropped      int64
	vertexBytes  int
	upload       time.Duration
	ticks        [benchmarkTickSamples]time.Duration
	frames       [benchmarkMeasuredFrames]time.Duration
	intervals    [benchmarkMeasuredFrames]time.Duration
}

var activeBenchmark *Benchmark

// StartBenchmark installs the sampler before starting the only graphical host
// in the process. It cancels the host after the last sampled submission.
func StartBenchmark(cancel context.CancelFunc) *Benchmark {
	b := &Benchmark{cancel: cancel}
	activeBenchmark = b
	return b
}

// BenchKeepActive allows the native scripted workload to keep running when
// another application takes focus. It does not bypass a zero-sized drawable.
func BenchKeepActive() bool { return activeBenchmark != nil }

// BenchFrameStart marks the start of CPU work after the host wakes for a frame.
func BenchFrameStart() time.Time {
	if activeBenchmark == nil {
		return time.Time{}
	}
	return time.Now()
}

// BenchFrameEnd samples CPU work through submission, excluding swap or paint,
// and tracks the largest measured vertex batch.
func BenchFrameEnd(start time.Time, vertexBytes int) {
	b := activeBenchmark
	if b == nil || b.framesSeen == benchmarkTotalFrames {
		return
	}
	elapsed := time.Since(start)
	if b.measuring() {
		i := b.framesSeen - benchmarkWarmupFrames
		b.frames[i] = elapsed
		// The last warmup start also makes the first measured interval valid.
		b.intervals[i] = start.Sub(b.lastStart)
		b.vertexBytes = max(b.vertexBytes, vertexBytes)
	}
	b.lastStart = start
	b.framesSeen++
	if b.framesSeen == benchmarkTotalFrames {
		b.cancel()
	}
}

// BenchUploadStart marks the start of the one-time texture upload submission.
func BenchUploadStart() time.Time {
	if activeBenchmark == nil {
		return time.Time{}
	}
	return time.Now()
}

// BenchUploadEnd records CPU time through submission, not GPU completion.
func BenchUploadEnd(start time.Time) {
	if b := activeBenchmark; b != nil {
		b.upload = time.Since(start)
	}
}

// BenchDropped counts scheduler debt discarded during the sample window.
func BenchDropped(dropped int64) {
	if b := activeBenchmark; b != nil && b.measuring() {
		b.dropped += dropped
	}
}

// Time a complete fixed tick in either driver path, including optional
// diagnostics. A warmup tick does not need a clock sample.
func benchTickStart() time.Time {
	if b := activeBenchmark; b != nil && b.measuring() {
		return time.Now()
	}
	return time.Time{}
}

func benchTickEnd(start time.Time) {
	if start.IsZero() {
		return
	}
	elapsed := time.Since(start)
	b := activeBenchmark
	if b.tickCount == len(b.ticks) {
		b.tickOverflow++
		return
	}
	b.ticks[b.tickCount] = elapsed
	b.tickCount++
}

func (b *Benchmark) measuring() bool {
	return b.framesSeen >= benchmarkWarmupFrames && b.framesSeen < benchmarkTotalFrames
}

// BenchmarkReport summarizes independent timings and drops in the sample window.
type BenchmarkReport struct {
	WarmupFrames       int             `json:"warmup_frames"`
	Tick               BenchmarkTiming `json:"tick"`
	Frame              BenchmarkTiming `json:"frame"`
	Interval           BenchmarkTiming `json:"interval"`
	DroppedTicks       int64           `json:"dropped_ticks"`
	PeakVertices       int             `json:"peak_vertices"`
	PeakVertexBytes    int             `json:"peak_vertex_bytes"`
	TextureUploadNS    int64           `json:"texture_upload_ns"`
	TickSampleOverflow int             `json:"tick_sample_overflow"`
	Clock              BenchmarkClock  `json:"clock"`
}

// BenchmarkTiming contains nearest-rank percentiles in nanoseconds. ZeroSamples
// makes durations below the observed clock resolution visible in the report.
type BenchmarkTiming struct {
	Samples     int   `json:"samples"`
	ZeroSamples int   `json:"zero_samples"`
	MedianNS    int64 `json:"median_ns"`
	P95NS       int64 `json:"p95_ns"`
	P99NS       int64 `json:"p99_ns"`
}

// BenchmarkClock describes consecutive reads of the same benchmark clock after
// the run. MinPositiveNS estimates its observable step, not the cost of a read.
// On Wasm, the browser can coarsen performance.now() before Go sees the value.
type BenchmarkClock struct {
	ReadPairs     int   `json:"read_pairs"`
	ZeroDeltas    int   `json:"zero_deltas"`
	MinPositiveNS int64 `json:"min_positive_ns"`
}

// Report sorts the recorded durations in place and observes clock granularity.
// Call it after host shutdown, so this work cannot affect the timed samples.
func (b *Benchmark) Report() (BenchmarkReport, error) {
	if b.framesSeen != benchmarkTotalFrames || b.tickCount == 0 {
		return BenchmarkReport{}, fmt.Errorf(
			"incomplete benchmark: %d/%d frames and %d measured ticks; keep the window focused",
			b.framesSeen, benchmarkTotalFrames, b.tickCount,
		)
	}
	return BenchmarkReport{
		WarmupFrames:       benchmarkWarmupFrames,
		Tick:               summarizeBenchmarkTimings(b.ticks[:b.tickCount]),
		Frame:              summarizeBenchmarkTimings(b.frames[:]),
		Interval:           summarizeBenchmarkTimings(b.intervals[:]),
		DroppedTicks:       b.dropped,
		PeakVertices:       b.vertexBytes / VertexStride,
		PeakVertexBytes:    b.vertexBytes,
		TextureUploadNS:    b.upload.Nanoseconds(),
		TickSampleOverflow: b.tickOverflow,
		Clock:              probeBenchmarkClock(),
	}, nil
}

func summarizeBenchmarkTimings(samples []time.Duration) BenchmarkTiming {
	slices.Sort(samples)
	zeroCount := 0
	for _, sample := range samples {
		if sample == 0 {
			zeroCount++
		}
	}
	return BenchmarkTiming{
		Samples:     len(samples),
		ZeroSamples: zeroCount,
		MedianNS:    benchmarkPercentile(samples, 50),
		P95NS:       benchmarkPercentile(samples, 95),
		P99NS:       benchmarkPercentile(samples, 99),
	}
}

func benchmarkPercentile(sorted []time.Duration, pct int) int64 {
	return sorted[(pct*len(sorted)+99)/100-1].Nanoseconds()
}

func probeBenchmarkClock() BenchmarkClock {
	result := BenchmarkClock{ReadPairs: benchmarkClockReads}
	previous := time.Now()
	for range benchmarkClockReads {
		now := time.Now()
		delta := now.Sub(previous).Nanoseconds()
		previous = now
		if delta == 0 {
			result.ZeroDeltas++
		} else if delta > 0 && (result.MinPositiveNS == 0 || delta < result.MinPositiveNS) {
			result.MinPositiveNS = delta
		}
	}
	return result
}

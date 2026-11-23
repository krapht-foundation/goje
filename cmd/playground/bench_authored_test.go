//go:build goje_bench && !goje_smoke

package main

import (
	"testing"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

// Each leg issues a real move command, then lets the fixed ticks and draw
// batch run through the authored detour before commanding the return leg.
type authoredBenchmark struct{ *authoredCourtyard }

func (s *authoredBenchmark) Update(tick engine.Tick) (engine.Transition, error) {
	if !s.selected {
		tick.Input.PrimaryPress = authoredPress(s.authoredCourtyard, 2.5, 2.5, 12)
	}
	if s.next == len(s.route) {
		x := 6.5
		if s.cell.X == 6 {
			x = 2.5
		}
		tick.Input.SecondaryPress = authoredPress(s.authoredCourtyard, x, 2.5, 0)
	}
	return s.authoredCourtyard.Update(tick)
}

func BenchmarkAuthoredAdvance(b *testing.B) {
	factory, err := authoredFactory()
	if err != nil {
		b.Fatal(err)
	}
	session, err := driver.Start(b.Context(), func() engine.Scene {
		return &authoredBenchmark{factory().(*authoredCourtyard)}
	}, driver.Config{ImageSize: groundRegion.Union(markerRegion).Size()})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := session.Stop(); err != nil {
			b.Error(err)
		}
	})
	const tickStep = time.Second/60 + time.Nanosecond
	var stamp time.Duration
	// Warm both legs and all marker and batch storage before measuring.
	for range 2*8*scoutEdgeTicks + 1 {
		if _, err := session.Advance(stamp); err != nil {
			b.Fatal(err)
		}
		stamp += tickStep
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := session.Advance(stamp); err != nil {
			b.Fatal(err)
		}
		stamp += tickStep
	}
}

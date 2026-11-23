//go:build goje_bench && !goje_smoke

package main

import (
	"testing"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/driver"
)

// BenchmarkCourtyardAdvance includes the scripted tick and recording the actual
// draw batch, but not GPU submission or platform event handling.
func BenchmarkCourtyardAdvance(b *testing.B) {
	for _, fixture := range []struct {
		name  string
		count int
	}{
		{name: "3-items", count: 3},
		{name: "2048-items", count: benchSprites},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			session, err := driver.Start(b.Context(), func() engine.Scene {
				return newBenchmarkCourtyard(fixture.count)
			}, driver.Config{ImageSize: markerRegion.Size()})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				if err := session.Stop(); err != nil {
					b.Error(err)
				}
			})
			const step = time.Second/60 + time.Nanosecond
			var stamp time.Duration
			// Run through a full patrol to grow the batch with and without a marker.
			for range 2*patrolLegTicks + 1 {
				if _, err := session.Advance(stamp); err != nil {
					b.Fatal(err)
				}
				stamp += step
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := session.Advance(stamp); err != nil {
					b.Fatal(err)
				}
				stamp += step
			}
		})
	}
}

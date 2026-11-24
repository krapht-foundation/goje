//go:build goje_bench && !goje_smoke

package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/internal/platform/driver"
	"github.com/krapht-foundation/goje/tile"
)

func BenchmarkGroundAdvance(b *testing.B) {
	for _, board := range []struct {
		name  string
		dense bool
	}{{"courtyard", false}, {"dense", true}} {
		for _, mode := range []struct {
			name  string
			tiles bool
		}{{"off", false}, {"on", true}} {
			b.Run(board.name+"/"+mode.name, func(b *testing.B) {
				benchmarkGroundAdvance(b, board.dense, mode.tiles)
			})
		}
	}
}

func benchmarkGroundAdvance(b *testing.B, dense, tiles bool) {
	b.Helper()
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		b.Fatal(err)
	}
	factory, err := groundBenchmarkFactory(loaded.layout, dense, tiles)
	if err != nil {
		b.Fatal(err)
	}
	atlas := driver.Config{ImageSize: groundRegion.Union(markerRegion).Size()}
	session, err := driver.Start(b.Context(), factory, atlas)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := session.Stop(); err != nil {
			b.Error(err)
		}
	})
	// Grow the frame buffer before measuring fixed ticks and batch recording.
	if _, err := session.Advance(0); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	var stamp time.Duration
	for b.Loop() {
		stamp += time.Second/60 + time.Nanosecond
		if _, err := session.Advance(stamp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGroundStartup(b *testing.B) {
	data, err := authoredFiles.ReadFile("art/ground.png")
	if err != nil {
		b.Fatal(err)
	}
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := tile.Decode(bytes.NewReader(data)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("pack", func(b *testing.B) {
		pixels, err := tile.Decode(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			playgroundConfig(pixels)
		}
	})
}

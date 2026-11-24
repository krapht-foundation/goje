//go:build goje_bench && !goje_smoke

package main

import (
	"bytes"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/internal/platform/driver"
	projectdata "github.com/krapht-foundation/goje/project"
	"github.com/krapht-foundation/goje/sprite"
)

const spriteBenchmarkTick = time.Second/60 + time.Nanosecond

func TestSpriteBenchmarkSubmitsOneQuadPerActor(t *testing.T) {
	for _, count := range []int{1, benchSprites} {
		fixed := startSpriteBenchmark(t, count, false)
		animated := startSpriteBenchmark(t, count, true)
		first := slices.Clone(spriteBatch(t, fixed, 0))
		if !bytes.Equal(first, spriteBatch(t, animated, 0)) {
			t.Fatalf("%d actors: modes start with different frames", count)
		}
		if count == benchSprites {
			one := startSpriteBenchmark(t, 1, false)
			// A quad is two triangles. Every other draw comes from the same ground pass.
			if got := len(first) - len(spriteBatch(t, one, 0)); got != (count-1)*6*driver.VertexStride {
				t.Fatalf("additional actors submitted %d vertex bytes", got)
			}
		}
		for step := 1; step <= 5; step++ {
			stamp := time.Duration(step) * spriteBenchmarkTick
			if !bytes.Equal(first, spriteBatch(t, fixed, stamp)) {
				t.Fatalf("%d actors: fixed batch changed on tick %d", count, step)
			}
			next := spriteBatch(t, animated, stamp)
			if len(next) != len(first) {
				t.Fatalf("%d actors: animated batch changed size on tick %d", count, step)
			}
			if step == 5 && bytes.Equal(first, next) {
				t.Fatalf("%d actors: animated batch did not enter the hop frame", count)
			}
		}
	}
}

func BenchmarkSpriteAdvance(b *testing.B) {
	for _, count := range []int{1, benchSprites} {
		for _, mode := range []struct {
			name     string
			animated bool
		}{{"fixed", false}, {"animated", true}} {
			b.Run(strconv.Itoa(count)+"/"+mode.name, func(b *testing.B) {
				s := startSpriteBenchmark(b, count, mode.animated)
				spriteBatch(b, s, 0)
				b.ReportAllocs()
				var stamp time.Duration
				for b.Loop() {
					stamp += spriteBenchmarkTick
					if _, err := s.Advance(stamp); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func startSpriteBenchmark(tb testing.TB, count int, animated bool) *driver.Session {
	tb.Helper()
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		tb.Fatal(err)
	}
	factory := spriteBenchmarkFactory(loaded, count, animated)
	config := driver.Config{ImageSize: loaded.atlas.Bounds().Size()}
	s, err := driver.Start(tb.Context(), factory, config)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := s.Stop(); err != nil {
			tb.Error(err)
		}
	})
	return s
}

func spriteBatch(tb testing.TB, s *driver.Session, stamp time.Duration) []byte {
	tb.Helper()
	frame, err := s.Advance(stamp)
	if err != nil {
		tb.Fatal(err)
	}
	return frame.Vertices
}

func BenchmarkSpriteStartup(b *testing.B) {
	pixels, err := authoredFiles.ReadFile("art/tomato.png")
	if err != nil {
		b.Fatal(err)
	}
	clips, err := authoredFiles.ReadFile("art/tomato.json")
	if err != nil {
		b.Fatal(err)
	}
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := sprite.Decode(bytes.NewReader(pixels), bytes.NewReader(clips)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("pack", func(b *testing.B) {
		loaded, err := projectdata.Load(authoredFiles)
		if err != nil {
			b.Fatal(err)
		}
		sheet, err := sprite.Decode(bytes.NewReader(pixels), bytes.NewReader(clips))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			packCourtyardAtlas(loaded.Ground, sheet.Image())
		}
	})
}

package fixed_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/krapht-foundation/goje/engine/fixed"
)

func TestWithLengthPointsAlongTheVectorWithinOneUnit(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(11, 12))
	for range 20000 {
		// Board-sized vectors, short ones, and every sign.
		span := []int{8, 600, 32768}[rng.IntN(3)]
		v := fixed.Vec2{
			X: fixed.Num(rng.IntN(2*span+1) - span),
			Y: fixed.Num(rng.IntN(2*span+1) - span),
		}
		n := fixed.Num(rng.IntN(2000) - 500)
		got := v.WithLength(n)
		if v == (fixed.Vec2{}) {
			if got != (fixed.Vec2{}) {
				t.Fatalf("zero vector scaled to %v", got)
			}
			continue
		}
		length := math.Hypot(float64(v.X), float64(v.Y))
		wantX, wantY := float64(v.X)*float64(n)/length, float64(v.Y)*float64(n)/length
		if math.Abs(float64(got.X)-wantX) > 1 || math.Abs(float64(got.Y)-wantY) > 1 {
			t.Fatalf("%v.WithLength(%d) = %v, want about (%.2f, %.2f)", v, n, got, wantX, wantY)
		}
	}
}

func TestWithLengthKeepsAxesAndNegatesForNegativeLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    fixed.Vec2
		n    fixed.Num
		want fixed.Vec2
	}{
		{"along X", fixed.Vec2{X: 5000}, 64, fixed.Vec2{X: 64}},
		{"along negative Y", fixed.Vec2{Y: -3}, 64, fixed.Vec2{Y: -64}},
		{"negative length reverses", fixed.Vec2{X: 100}, -64, fixed.Vec2{X: -64}},
		// The 3-4-5 triangle scaled to length 5 units (1280) is exact.
		{"3-4-5", fixed.Vec2{X: 768, Y: 1024}, 1280, fixed.Vec2{X: 768, Y: 1024}},
		{"zero length", fixed.Vec2{X: 5, Y: 5}, 0, fixed.Vec2{}},
	}
	for _, tt := range tests {
		if got := tt.v.WithLength(tt.n); got != tt.want {
			t.Errorf("%s: %v.WithLength(%d) = %v, want %v", tt.name, tt.v, tt.n, got, tt.want)
		}
	}
}

func TestWithLengthHandlesTheLargestVectorsWithoutOverflow(t *testing.T) {
	t.Parallel()

	huge := fixed.Vec2{X: fixed.Max, Y: fixed.Min}
	got := huge.WithLength(fixed.FromInt(2))
	// Direction is (1, −1)/√2, so each component is about ±362 units.
	if got.X < 360 || got.X > 364 || got.Y > -360 || got.Y < -364 {
		t.Errorf("WithLength on extreme components = %v, want about (362, −362)", got)
	}
	if got := (fixed.Vec2{X: 1}).WithLength(fixed.Max); got.X != fixed.Max || got.Y != 0 {
		t.Errorf("length at the Num limit = %v", got)
	}
}

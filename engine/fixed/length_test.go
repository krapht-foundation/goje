package fixed_test

import (
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/krapht-foundation/goje/engine/fixed"
)

// exactBounds reports whether r is the component c·n/√(x²+y²) rounded to the
// nearest integer, ties away from zero, by the definition of the rounding: for
// the magnitude r ≥ 0, (2r−1)²·sq ≤ 4·(|c|·|n|)² < (2r+1)²·sq, where the left
// inequality is vacuous for r = 0. It uses big integers, not the production code.
func exactBounds(r, c, n, sq int64) bool {
	a := new(big.Int).Mul(big.NewInt(abs(c)), big.NewInt(abs(n)))
	target := new(big.Int).Mul(a, a)
	target.Lsh(target, 2)
	scaled := func(k int64) *big.Int {
		return new(big.Int).Mul(new(big.Int).Mul(big.NewInt(k), big.NewInt(k)), big.NewInt(sq))
	}
	below := r == 0 || scaled(2*r-1).Cmp(target) <= 0
	return below && target.Cmp(scaled(2*r+1)) < 0
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestWithLengthIsTheExactlyRoundedComponent(t *testing.T) {
	t.Parallel()

	// Every small vector and length, with the nearest integer found by trying them all.
	for x := int64(-20); x <= 20; x++ {
		for y := int64(-20); y <= 20; y++ {
			if x == 0 && y == 0 {
				continue
			}
			sq := x*x + y*y
			for n := int64(-40); n <= 40; n++ {
				got := fixed.Vec2{X: fixed.Num(x), Y: fixed.Num(y)}.WithLength(fixed.Num(n))
				for _, tc := range []struct{ c, got int64 }{{x, int64(got.X)}, {y, int64(got.Y)}} {
					magnitude := abs(tc.got)
					want := int64(-1)
					for r := int64(0); r <= abs(n); r++ {
						if exactBounds(r, tc.c, n, sq) {
							want = r
							break
						}
					}
					wantSign := int64(1)
					if (tc.c < 0) != (n < 0) {
						wantSign = -1
					}
					if want < 0 || magnitude != want || (want != 0 && tc.got/magnitude != wantSign) {
						t.Fatalf("(%d, %d).WithLength(%d) component %d = %d, want %d with sign %d",
							x, y, n, tc.c, tc.got, want, wantSign)
					}
				}
			}
		}
	}
}

func TestWithLengthIsExactForEveryMagnitude(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(11, 12))
	for range 20000 {
		// Board-sized vectors, short ones, and the full range, with every sign.
		span := []int64{8, 600, 32768, 1 << 31}[rng.IntN(4)]
		x, y := rng.Int64N(2*span)-span, rng.Int64N(2*span)-span
		x, y = min(x, int64(fixed.Max)), min(y, int64(fixed.Max))
		n := rng.Int64N(1<<20) - 1<<18
		if rng.IntN(8) == 0 {
			n = int64([]fixed.Num{fixed.Max, fixed.Min}[rng.IntN(2)])
		}
		got := fixed.Vec2{X: fixed.Num(x), Y: fixed.Num(y)}.WithLength(fixed.Num(n))
		if x == 0 && y == 0 {
			if got != (fixed.Vec2{}) {
				t.Fatalf("zero vector scaled to %v", got)
			}
			continue
		}
		sq := x*x + y*y
		for _, tc := range []struct{ c, got int64 }{{x, int64(got.X)}, {y, int64(got.Y)}} {
			// A magnitude of 2^31 saturates, so compare what fits.
			if m := abs(tc.got); m < 1<<31-1 && !exactBounds(m, tc.c, n, sq) {
				t.Fatalf(
					"(%d, %d).WithLength(%d) component %d = %d is not the nearest integer",
					x,
					y,
					n,
					tc.c,
					tc.got,
				)
			}
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

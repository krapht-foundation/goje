// Package fixed provides exact, deterministic arithmetic for simulation code.
//
// Floating-point results may differ between architectures because a compiler
// is allowed to fuse a multiply and an add. Integer arithmetic does not, so a
// simulation built on this package produces identical results on every desktop
// and browser target. Draw code converts to float64 once, at the presentation
// boundary, and simulation never converts back.
package fixed

import (
	"math"
	"math/bits"
)

// Frac is the number of fractional bits in a Num.
const Frac = 8

// One is the Num that represents 1.
const One Num = 1 << Frac

// scale is One as a float64, for conversions.
const scale = float64(One)

const (
	// Max is the largest Num.
	Max Num = math.MaxInt32
	// Min is the smallest Num.
	Min Num = math.MinInt32
)

// Num is a signed count of 1/256 units. Its arithmetic saturates at Min and
// Max instead of wrapping, so an overflow is deterministic and never flips a
// sign. The zero value is 0.
type Num int32

// FromInt returns n whole units, saturating when n is out of range.
func FromInt(n int) Num {
	return saturate(int64(n) << Frac)
}

// FromFloat rounds f to the nearest unit, with ties away from zero. It returns
// false for NaN, infinities, and values outside the Num range. Use it to read
// authored data and in tests; simulation code never needs it.
func FromFloat(f float64) (Num, bool) {
	scaled := math.Round(f * scale)
	if math.IsNaN(scaled) || scaled < float64(Min) || scaled > float64(Max) {
		return 0, false
	}
	return Num(scaled), true
}

// Float64 returns the exact value of n for drawing.
func (n Num) Float64() float64 {
	return float64(n) / scale
}

// Floor returns the largest whole unit not greater than n. For a tile
// position it is the index of the tile that contains it.
func (n Num) Floor() int {
	return int(n >> Frac)
}

// Neg returns −n. Min saturates to Max.
func (n Num) Neg() Num {
	return saturate(-int64(n))
}

// Abs returns |n|. Min saturates to Max.
func (n Num) Abs() Num {
	if n < 0 {
		return n.Neg()
	}
	return n
}

// Add returns n+m, saturating on overflow.
func (n Num) Add(m Num) Num {
	return saturate(int64(n) + int64(m))
}

// Sub returns n−m, saturating on overflow.
func (n Num) Sub(m Num) Num {
	return saturate(int64(n) - int64(m))
}

// Mul returns n×m rounded to the nearest unit, with ties away from zero, and
// saturating on overflow.
func (n Num) Mul(m Num) Num {
	product := int64(n) * int64(m)
	half := int64(One) / 2
	if product < 0 {
		return saturate(-((-product + half) >> Frac))
	}
	return saturate((product + half) >> Frac)
}

// Div returns n÷m rounded to the nearest unit, with ties away from zero, and
// saturating on overflow. Division by zero saturates toward the sign of n, and
// 0÷0 is 0, so the result is always defined.
func (n Num) Div(m Num) Num {
	if m == 0 {
		switch {
		case n > 0:
			return Max
		case n < 0:
			return Min
		}
		return 0
	}
	numerator, denominator := int64(n)<<Frac, int64(m)
	negative := (numerator < 0) != (denominator < 0)
	if numerator < 0 {
		numerator = -numerator
	}
	if denominator < 0 {
		denominator = -denominator
	}
	quotient := (2*numerator + denominator) / (2 * denominator)
	if negative {
		quotient = -quotient
	}
	return saturate(quotient)
}

func saturate(v int64) Num {
	switch {
	case v > int64(Max):
		return Max
	case v < int64(Min):
		return Min
	}
	return Num(v)
}

// Vec2 is a pair of Num. On the ground plane X and Y match the axes of
// engine.WorldItem.GroundX and GroundY.
type Vec2 struct{ X, Y Num }

// Add returns the component-wise sum, saturating on overflow.
func (v Vec2) Add(w Vec2) Vec2 { return Vec2{v.X.Add(w.X), v.Y.Add(w.Y)} }

// Sub returns the component-wise difference, saturating on overflow.
func (v Vec2) Sub(w Vec2) Vec2 { return Vec2{v.X.Sub(w.X), v.Y.Sub(w.Y)} }

// WithLength returns the vector with the direction of v and length n, so a
// caller can aim at a target without a floating-point square root. Each
// component is the exact value x·n/√(x²+y²) rounded to the nearest unit, with
// ties away from zero, and saturated at the ends of the range. A zero vector
// stays zero, and a negative n points the other way. It uses only integer
// arithmetic and no approximation of the square root, for every vector.
func (v Vec2) WithLength(n Num) Vec2 {
	x, y := int64(v.X), int64(v.Y)
	if (x == 0 && y == 0) || n == 0 {
		return Vec2{}
	}
	sq := uint64(x*x + y*y)    // #nosec G115 -- the sum is non-negative and at most 2^63.
	m := uint64(abs(int64(n))) // #nosec G115 -- the absolute value is non-negative.
	return Vec2{
		X: scaledComponent(x, m, sq, n < 0),
		Y: scaledComponent(y, m, sq, n < 0),
	}
}

// scaledComponent returns c·m/√sq rounded to the nearest integer, ties away
// from zero, with the sign of c, negated if negate is set. The magnitude r is
// the largest integer whose lower rounding boundary r−½ does not exceed the
// exact value q = |c|·m/√sq. Comparing squares keeps that test exact:
// (2r−1)²·sq ≤ 4·(|c|·m)². Both sides fit in 128 bits.
func scaledComponent(c int64, m, sq uint64, negate bool) Num {
	a := uint64(
		abs(c),
	) * m // #nosec G115 -- the absolute value is non-negative; the product is at most 2^62.
	rhsHi, rhsLo := bits.Mul64(a, a)
	rhsHi, rhsLo = rhsHi<<2|rhsLo>>62, rhsLo<<2
	// q never exceeds m, because |c| ≤ √sq, so the answer lies in [0, m].
	lo, hi := uint64(0), m
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		odd := 2*mid - 1 // At most 2^32 − 1, so its square fits in 64 bits.
		lhsHi, lhsLo := bits.Mul64(odd*odd, sq)
		if lhsHi < rhsHi || (lhsHi == rhsHi && lhsLo <= rhsLo) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	r := int64(lo) // #nosec G115 -- lo ≤ m ≤ 2^31.
	if (c < 0) != negate {
		r = -r
	}
	return saturate(r)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Rect is the half-open rectangle [Min.X, Max.X) × [Min.Y, Max.Y). A rectangle
// with Max.X ≤ Min.X or Max.Y ≤ Min.Y is empty and overlaps nothing.
type Rect struct{ Min, Max Vec2 }

// Empty reports whether r contains no points.
func (r Rect) Empty() bool {
	return r.Max.X <= r.Min.X || r.Max.Y <= r.Min.Y
}

// Overlaps reports whether r and s share any point. Rectangles that only touch
// along an edge or at a corner do not overlap.
func (r Rect) Overlaps(s Rect) bool {
	return !r.Empty() && !s.Empty() &&
		r.Min.X < s.Max.X && s.Min.X < r.Max.X &&
		r.Min.Y < s.Max.Y && s.Min.Y < r.Max.Y
}

// Translate returns r moved by v, saturating on overflow.
func (r Rect) Translate(v Vec2) Rect {
	return Rect{r.Min.Add(v), r.Max.Add(v)}
}

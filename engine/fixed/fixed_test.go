package fixed_test

import (
	"math"
	"testing"

	"github.com/krapht-foundation/goje/engine/fixed"
)

func TestArithmeticRoundsToNearestAndTiesAwayFromZero(t *testing.T) {
	t.Parallel()

	half := fixed.One / 2
	tests := []struct {
		name string
		got  fixed.Num
		want fixed.Num
	}{
		{"whole product", fixed.FromInt(3).Mul(fixed.FromInt(-4)), fixed.FromInt(-12)},
		{"fraction product", half.Mul(half), fixed.One / 4},
		// 1/256 × 1/2 is 1/512 of a unit: a tie that rounds away from zero.
		{"tie rounds up", fixed.Num(1).Mul(half), 1},
		{"tie rounds down", fixed.Num(-1).Mul(half), -1},
		// 1/256 × 1/4 is 1/1024: below half, so it rounds to zero.
		{"below half rounds to zero", fixed.Num(1).Mul(fixed.One / 4), 0},
		{"whole quotient", fixed.FromInt(-12).Div(fixed.FromInt(4)), fixed.FromInt(-3)},
		// 1/256 ÷ 2 is half a unit: a tie that rounds away from zero.
		{"quotient tie up", fixed.Num(1).Div(fixed.FromInt(2)), 1},
		{"quotient tie down", fixed.Num(-1).Div(fixed.FromInt(2)), -1},
		// 1 ÷ 3 = 85.33 units rounds to 85; 2 ÷ 3 = 170.67 rounds to 171.
		{"third", fixed.One.Div(fixed.FromInt(3)), 85},
		{"two thirds", fixed.FromInt(2).Div(fixed.FromInt(3)), 171},
		{"negative divisor", fixed.One.Div(fixed.FromInt(-3)), -85},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %d, want %d", test.name, test.got, test.want)
		}
	}
}

func TestArithmeticSaturatesAtBothEnds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  fixed.Num
		want fixed.Num
	}{
		{"add high", fixed.Max.Add(1), fixed.Max},
		{"add low", fixed.Min.Add(-1), fixed.Min},
		{"sub high", fixed.Max.Sub(-1), fixed.Max},
		{"sub low", fixed.Min.Sub(1), fixed.Min},
		{"neg min", fixed.Min.Neg(), fixed.Max},
		{"abs min", fixed.Min.Abs(), fixed.Max},
		{"mul high", fixed.Max.Mul(fixed.FromInt(2)), fixed.Max},
		{"mul low", fixed.Min.Mul(fixed.FromInt(2)), fixed.Min},
		{"mul sign", fixed.Min.Mul(fixed.FromInt(-2)), fixed.Max},
		{"div high", fixed.Max.Div(fixed.Num(1)), fixed.Max},
		{"div by zero positive", fixed.One.Div(0), fixed.Max},
		{"div by zero negative", fixed.FromInt(-1).Div(0), fixed.Min},
		{"zero over zero", fixed.Num(0).Div(0), 0},
		{"from int high", fixed.FromInt(math.MaxInt32), fixed.Max},
		{"from int low", fixed.FromInt(math.MinInt32), fixed.Min},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %d, want %d", test.name, test.got, test.want)
		}
	}
}

func TestFromFloatRoundsAndRejectsUnrepresentableValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		in   float64
		want fixed.Num
	}{
		{0, 0},
		{1.5, 384},
		{-1.5, -384},
		{0.5 / 256, 1},
		{-0.5 / 256, -1},
		{0.4 / 256, 0},
	} {
		got, ok := fixed.FromFloat(test.in)
		if !ok || got != test.want {
			t.Errorf("FromFloat(%v) = %d, %v; want %d, true", test.in, got, ok, test.want)
		}
	}
	for _, in := range []float64{
		math.NaN(), math.Inf(1), math.Inf(-1), 1e12, -1e12, float64(fixed.Max)/256 + 1,
	} {
		if got, ok := fixed.FromFloat(in); ok {
			t.Errorf("FromFloat(%v) = %d, true; want rejection", in, got)
		}
	}
}

func TestFloat64AndFloorAreExact(t *testing.T) {
	t.Parallel()

	n, _ := fixed.FromFloat(2.75)
	if n.Float64() != 2.75 {
		t.Errorf("Float64() = %v, want 2.75", n.Float64())
	}
	for _, test := range []struct {
		in   fixed.Num
		want int
	}{{0, 0}, {255, 0}, {256, 1}, {-1, -1}, {-256, -1}, {-257, -2}} {
		if got := test.in.Floor(); got != test.want {
			t.Errorf("Floor(%d) = %d, want %d", test.in, got, test.want)
		}
	}
}

func TestRectEmptinessAndOverlap(t *testing.T) {
	t.Parallel()

	rect := func(x0, y0, x1, y1 int) fixed.Rect {
		return fixed.Rect{
			Min: fixed.Vec2{X: fixed.FromInt(x0), Y: fixed.FromInt(y0)},
			Max: fixed.Vec2{X: fixed.FromInt(x1), Y: fixed.FromInt(y1)},
		}
	}
	base := rect(0, 0, 2, 2)
	for _, test := range []struct {
		name  string
		other fixed.Rect
		want  bool
	}{
		{"inside", rect(1, 1, 2, 2), true},
		{"partial", rect(1, 1, 3, 3), true},
		{"touching edge", rect(2, 0, 4, 2), false},
		{"touching corner", rect(2, 2, 3, 3), false},
		{"apart", rect(3, 0, 4, 2), false},
		{"empty", rect(1, 1, 1, 2), false},
		{"inverted", rect(2, 2, 1, 1), false},
	} {
		if got := base.Overlaps(test.other); got != test.want {
			t.Errorf("%s: Overlaps = %v, want %v", test.name, got, test.want)
		}
		if got := test.other.Overlaps(base); got != test.want {
			t.Errorf("%s: reversed Overlaps = %v, want %v", test.name, got, test.want)
		}
	}
	if !rect(1, 1, 1, 5).Empty() || !rect(1, 1, 5, 1).Empty() || base.Empty() {
		t.Error("Empty misclassified a rectangle")
	}
	moved := base.Translate(fixed.Vec2{X: fixed.FromInt(1), Y: fixed.FromInt(-1)})
	if moved != rect(1, -1, 3, 1) {
		t.Errorf("Translate = %v", moved)
	}
}

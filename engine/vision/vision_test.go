package vision

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

// board is a test grid. Cells outside it are blocked.
type board struct {
	w, h  int
	walls []bool
}

func (b board) blocked(x, y int) bool {
	return x < 0 || y < 0 || x >= b.w || y >= b.h || b.walls[y*b.w+x]
}

func randomBoard(rng *rand.Rand) board {
	b := board{w: rng.IntN(16) + 1, h: rng.IntN(16) + 1}
	density := rng.IntN(5)
	b.walls = make([]bool, b.w*b.h)
	for i := range b.walls {
		b.walls[i] = rng.IntN(10) < density
	}
	return b
}

// cast returns the cells Cast visits, in order, and fails if one repeats.
func cast(tb testing.TB, b board, origin grid.Cell, radius int) []grid.Cell {
	tb.Helper()
	var got []grid.Cell
	Cast(origin, radius, b.blocked, func(x, y int) { got = append(got, grid.Cell{X: x, Y: y}) })
	seen := map[grid.Cell]bool{}
	for _, c := range got {
		if seen[c] {
			tb.Fatalf("Cast from %v radius %d visited %v twice", origin, radius, c)
		}
		seen[c] = true
	}
	if len(got) == 0 || got[0] != origin {
		tb.Fatalf("Cast from %v did not visit the origin first", origin)
	}
	return got
}

// ratio is a slope n/d with d > 0.
type ratio struct{ n, d int }

func (r ratio) less(o ratio) bool { return r.n*o.d < o.n*r.d }

// toCell is the grid cell at a quadrant's depth and column, as Cast maps them.
func toCell(q int, origin grid.Cell, depth, col int) grid.Cell {
	c := caster{ox: origin.X, oy: origin.Y, quadrant: q}
	x, y := c.cell(depth, col)
	return grid.Cell{X: x, Y: y}
}

// clearRay reports whether the ray of slope r in quadrant q, which must not pass
// exactly between two cells of any row before depth, crosses only open cells
// in rows 1 to depth-1.
func clearRay(b board, q int, origin grid.Cell, r ratio, depth int) bool {
	if -r.d >= r.n || r.n >= r.d {
		return false // Outside the quadrant.
	}
	for d := 1; d < depth; d++ {
		col := floorDiv(2*r.n*d+r.d, 2*r.d)
		if c := toCell(q, origin, d, col); b.blocked(c.X, c.Y) {
			return false
		}
	}
	return true
}

// reference is the brute-force line-of-sight rule of specs/vision-m15.md. In
// each quadrant, a ray from the origin's center at slope m crosses row d at
// column m·d and is clear when every cell it crosses before the target row is
// open. The clear rays form open intervals between the slopes at which a ray
// passes exactly between two cells. An open cell is visible when its center's
// slope is in the closure of the clear rays; a blocked cell when a clear ray
// enters the open interval of slopes across it.
func reference(b board, origin grid.Cell, radius int) map[grid.Cell]bool {
	visible := map[grid.Cell]bool{origin: true}
	for q := range 4 {
		for depth := 1; depth <= radius; depth++ {
			for col := -depth; col <= depth; col++ {
				if depth*depth+col*col > radius*radius {
					continue
				}
				c := toCell(q, origin, depth, col)
				if b.blocked(c.X, c.Y) {
					if wallSeen(b, q, origin, depth, col) {
						visible[c] = true
					}
					continue
				}
				// Rays just beside the center: nearer than any slope at which
				// a ray passes between two cells of an earlier row.
				den := 8 * depth * depth
				if clearRay(b, q, origin, ratio{8*depth*col - 1, den}, depth) ||
					clearRay(b, q, origin, ratio{8*depth*col + 1, den}, depth) {
					visible[c] = true
				}
			}
		}
	}
	return visible
}

func wallSeen(b board, q int, origin grid.Cell, depth, col int) bool {
	lo, hi := ratio{2*col - 1, 2 * depth}, ratio{2*col + 1, 2 * depth}
	if lo.less(ratio{-1, 1}) {
		lo = ratio{-1, 1}
	}
	if (ratio{1, 1}).less(hi) {
		hi = ratio{1, 1}
	}
	points := []ratio{lo, hi}
	for d := 1; d < depth; d++ {
		for c := -d - 1; c <= d; c++ {
			if p := (ratio{2*c + 1, 2 * d}); lo.less(p) && p.less(hi) {
				points = append(points, p)
			}
		}
	}
	slices.SortFunc(points, func(a, b ratio) int {
		switch {
		case a.less(b):
			return -1
		case b.less(a):
			return 1
		}
		return 0
	})
	for i := 1; i < len(points); i++ {
		a, z := points[i-1], points[i]
		if mid := (ratio{a.n*z.d + z.n*a.d, 2 * a.d * z.d}); a.less(z) &&
			clearRay(b, q, origin, mid, depth) {
			return true
		}
	}
	return false
}

func TestCastMatchesBruteForceLineOfSight(t *testing.T) {
	rng := rand.New(rand.NewPCG(16, 15))
	for range 400 {
		b := randomBoard(rng)
		radius := rng.IntN(13)
		for range 4 {
			origin := grid.Cell{X: rng.IntN(b.w), Y: rng.IntN(b.h)}
			want := reference(b, origin, radius)
			got := cast(t, b, origin, radius)
			for _, c := range got {
				if !want[c] {
					t.Fatalf("%v radius %d: Cast saw %v, which the reference does not\n%s",
						origin, radius, c, b)
				}
			}
			if len(got) != len(want) {
				t.Fatalf("%v radius %d: Cast saw %d cells, the reference %d\n%s",
					origin, radius, len(got), len(want), b)
			}
		}
	}
}

func (b board) String() string {
	s := make([]byte, 0, (b.w+1)*b.h)
	for y := range b.h {
		for x := range b.w {
			s = append(s, map[bool]byte{true: '#', false: '.'}[b.walls[y*b.w+x]])
		}
		s = append(s, '\n')
	}
	return string(s)
}

func TestCastIsSymmetricBetweenOpenCells(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	for range 150 {
		b := randomBoard(rng)
		radius := rng.IntN(12) + 1
		sees := map[[2]grid.Cell]bool{}
		for y := range b.h {
			for x := range b.w {
				a := grid.Cell{X: x, Y: y}
				for _, c := range cast(t, b, a, radius) {
					sees[[2]grid.Cell{a, c}] = true
				}
			}
		}
		for pair := range sees {
			a, c := pair[0], pair[1]
			if !b.blocked(a.X, a.Y) && !b.blocked(c.X, c.Y) && !sees[[2]grid.Cell{c, a}] {
				t.Fatalf("radius %d: %v sees %v but not the reverse\n%s", radius, a, c, b)
			}
		}
	}
}

func TestCastBasics(t *testing.T) {
	open := board{w: 9, h: 9, walls: make([]bool, 81)}
	if got := cast(t, open, grid.Cell{X: 4, Y: 4}, 0); len(got) != 1 {
		t.Fatalf("radius 0 saw %v, want only the origin", got)
	}
	// On an open board, every cell within the radius is visible.
	if got := cast(t, open, grid.Cell{X: 4, Y: 4}, 4); len(got) != 49 {
		t.Fatalf("radius 4 on an open board saw %d cells, want 49", len(got))
	}
	// A wall is seen, and the cell straight behind it is not.
	walled := board{w: 5, h: 1, walls: []bool{false, false, true, false, false}}
	got := cast(t, walled, grid.Cell{X: 0, Y: 0}, 4)
	if !slices.Contains(got, grid.Cell{X: 2, Y: 0}) || slices.Contains(got, grid.Cell{X: 3, Y: 0}) {
		t.Fatalf("a corridor with a wall saw %v", got)
	}
	// The order is fixed: two casts visit the same cells in the same order.
	rng := rand.New(rand.NewPCG(1, 2))
	b := randomBoard(rng)
	if a, c := cast(t, b, grid.Cell{}, 9), cast(t, b, grid.Cell{}, 9); !slices.Equal(a, c) {
		t.Fatal("two casts visited cells in different orders")
	}
}

func TestCastDoesNotAllocate(t *testing.T) {
	b := board{w: 32, h: 32, walls: make([]bool, 32*32)}
	for i := range b.walls {
		b.walls[i] = i%7 == 0
	}
	n := 0
	visit := func(int, int) { n++ }
	if allocs := testing.AllocsPerRun(50, func() {
		Cast(grid.Cell{X: 16, Y: 16}, 12, b.blocked, visit)
	}); allocs != 0 {
		t.Fatalf("Cast allocated %v times", allocs)
	}
}

// FuzzCast checks that Cast never fails, visits each cell once, agrees with the
// reference, and is symmetric for the origin's open neighbors in sight.
func FuzzCast(f *testing.F) {
	f.Add([]byte{5, 5, 3, 2, 2, 0x10, 0x20, 0x44, 0x81})
	f.Add([]byte{15, 0, 9, 0, 0, 0xff, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 5 {
			return
		}
		b := board{w: int(data[0]%12) + 1, h: int(data[1]%12) + 1}
		radius := int(data[2]%14) - 1
		origin := grid.Cell{X: int(data[3]) % b.w, Y: int(data[4]) % b.h}
		b.walls = make([]bool, b.w*b.h)
		for i := range b.walls {
			if k := 5 + i/8; k < len(data) {
				b.walls[i] = data[k]>>(i%8)&1 == 1
			}
		}
		got := cast(t, b, origin, radius)
		if want := reference(b, origin, max(radius, 0)); len(got) != len(want) {
			t.Fatalf("Cast saw %d cells, the reference %d", len(got), len(want))
		}
		if b.blocked(origin.X, origin.Y) {
			return
		}
		for _, c := range got {
			if b.blocked(c.X, c.Y) {
				continue
			}
			if !slices.Contains(cast(t, b, c, radius), origin) {
				t.Fatalf("%v sees %v but not the reverse", origin, c)
			}
		}
	})
}

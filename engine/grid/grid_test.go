package grid_test

import (
	"math"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

func router(tb testing.TB, w, h int) *grid.Router {
	tb.Helper()
	r, err := grid.NewRouter(w, h)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

func TestCellAt(t *testing.T) {
	r := router(t, 4, 3)
	for _, c := range []struct {
		x, y float64
		want grid.Cell
		ok   bool
	}{
		{0, 0, grid.Cell{}, true},
		{0.99, 0.99, grid.Cell{}, true},
		{1, 2, grid.Cell{X: 1, Y: 2}, true},
		{math.Nextafter(4, 0), math.Nextafter(3, 0), grid.Cell{X: 3, Y: 2}, true},
		{-0.01, 1, grid.Cell{}, false},
		{1, -0.01, grid.Cell{}, false},
		{4, 1, grid.Cell{}, false},
		{1, 3, grid.Cell{}, false},
		{math.NaN(), 0, grid.Cell{}, false},
		{0, math.Inf(1), grid.Cell{}, false},
	} {
		if got, ok := r.CellAt(c.x, c.y); got != c.want || ok != c.ok {
			t.Errorf("CellAt(%v, %v) = %v, %t; want %v, %t", c.x, c.y, got, ok, c.want, c.ok)
		}
	}
}

func TestNewRouterRejectsInvalidDimensions(t *testing.T) {
	for _, size := range [][2]int{{0, 1}, {1, -1}, {129, 1}, {1, 129}, {math.MaxInt, math.MaxInt}} {
		if r, err := grid.NewRouter(size[0], size[1]); err == nil || r != nil {
			t.Errorf("accepted board size %v", size)
		}
	}
}

func TestRouteAroundWallWithTiedDetours(t *testing.T) {
	r := router(t, 5, 3)
	walkable := func(x, y int) bool { return x != 2 || y != 1 }
	start, goal := grid.Cell{Y: 1}, grid.Cell{X: 4, Y: 1}
	// The upper bypass is equally short; +Y wins after +X hits the wall.
	want := []grid.Cell{{X: 1, Y: 1}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, goal}
	if got, ok := r.AppendRoute(nil, start, goal, walkable); !ok || !slices.Equal(got, want) {
		t.Fatalf("detour = %v, %t; want %v", got, ok, want)
	}
	// Opening the wall changes the next query, even on the same router.
	want = []grid.Cell{{X: 1, Y: 1}, {X: 2, Y: 1}, {X: 3, Y: 1}, goal}
	if got, ok := r.AppendRoute(nil, start, goal, open); !ok || !slices.Equal(got, want) {
		t.Fatalf("direct route = %v, %t; want %v", got, ok, want)
	}
}

func TestRoutePrefersPositiveXThenNegativeXOverNegativeY(t *testing.T) {
	r := router(t, 3, 3)
	for _, c := range []struct{ start, step, goal grid.Cell }{
		{grid.Cell{}, grid.Cell{X: 1}, grid.Cell{X: 1, Y: 1}},
		{grid.Cell{X: 2, Y: 2}, grid.Cell{X: 1, Y: 2}, grid.Cell{X: 1, Y: 1}},
	} {
		want := []grid.Cell{c.step, c.goal}
		if got, ok := r.AppendRoute(nil, c.start, c.goal, open); !ok || !slices.Equal(got, want) {
			t.Errorf("route %v to %v = %v, %t; want %v", c.start, c.goal, got, ok, want)
		}
	}
}

func TestRoutePreservesBufferOnNoRoute(t *testing.T) {
	r := router(t, 3, 3)
	walkable := func(x, y int) bool {
		if x < 0 || y < 0 || x >= 3 || y >= 3 {
			t.Fatalf("walkability queried outside the board: (%d, %d)", x, y)
		}
		return x != 1
	}
	start := grid.Cell{}
	for _, c := range []struct {
		start, goal grid.Cell
		ok          bool
	}{
		{start, start, true},
		{start, grid.Cell{X: 1}, false},
		{grid.Cell{X: 1}, start, false},
		{grid.Cell{X: 1}, grid.Cell{X: 1}, false},
		{start, grid.Cell{X: 2}, false},
		{start, grid.Cell{X: -1}, false},
		{grid.Cell{Y: 3}, start, false},
	} {
		prefix := grid.Cell{X: 2, Y: 2}
		spare := grid.Cell{X: 2, Y: 1}
		buf := []grid.Cell{prefix, spare}
		got, ok := r.AppendRoute(buf[:1], c.start, c.goal, walkable)
		if ok != c.ok || !slices.Equal(got, []grid.Cell{prefix}) || buf[1] != spare {
			t.Errorf("%v to %v: unexpected route %v or status %t", c.start, c.goal, got, ok)
		}
	}
	buf := []grid.Cell{{X: 2, Y: 2}, {}}
	goal := grid.Cell{Y: 1}
	want := []grid.Cell{buf[0], goal}
	if got, ok := r.AppendRoute(buf[:1], start, goal, walkable); !ok || !slices.Equal(got, want) {
		t.Fatalf("append = %v, %t; want %v", got, ok, want)
	}
}

func TestRouteFullBoard(t *testing.T) {
	const last = 127
	r := router(t, last+1, last+1)
	start, goal := grid.Cell{}, grid.Cell{X: last, Y: last}
	dst := make([]grid.Cell, 0, 2*last)
	got, ok := r.AppendRoute(dst, start, goal, open)
	if !ok || len(got) != 2*last {
		t.Fatalf("full board route length = %d, %t; want %d steps", len(got), ok, 2*last)
	}
	for i, cell := range got {
		want := grid.Cell{X: min(i+1, last), Y: max(i+1-last, 0)}
		if cell != want {
			t.Fatalf("step %d = %v, want %v", i, cell, want)
		}
	}
	if got, ok := r.AppendRoute(dst, start, goal, isolatedCorner); ok || len(got) != 0 {
		t.Fatalf("isolated corner should be unreachable, got %d steps, %t", len(got), ok)
	}
}

func open(_, _ int) bool { return true }

func isolatedCorner(x, y int) bool {
	return (x != 126 || y != 127) && (x != 127 || y != 126)
}

func BenchmarkRoute(b *testing.B) {
	for _, c := range []struct {
		name     string
		walkable func(x, y int) bool
		want     bool
	}{
		{"opposite-corner", open, true},
		{"isolated-corner", isolatedCorner, false},
	} {
		b.Run(c.name, func(b *testing.B) {
			r := router(b, 128, 128)
			dst := make([]grid.Cell, 0, 254)
			for b.Loop() {
				path, ok := r.AppendRoute(dst, grid.Cell{}, grid.Cell{X: 127, Y: 127}, c.walkable)
				if ok != c.want || (ok && len(path) != 254) {
					b.Fatalf("route length = %d, reachable = %t; want reachable = %t", len(path), ok, c.want)
				}
			}
		})
	}
}

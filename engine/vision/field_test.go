package vision

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

func TestNewFieldRejectsBadSizes(t *testing.T) {
	open := func(int, int) bool { return false }
	for _, c := range [][4]int{{0, 4, 1, 3}, {4, 129, 1, 3}, {4, 4, 0, 3}, {4, 4, 1, -1}, {4, 4, 1, MaxRadius + 1}} {
		if _, err := NewField(c[0], c[1], c[2], c[3], open); err == nil {
			t.Errorf("NewField(%v) accepted bad arguments", c)
		}
	}
}

// TestFieldMatchesRecomputing moves and removes viewers at random and checks
// the counts and each viewer's cells against casting every viewer from scratch.
func TestFieldMatchesRecomputing(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 9))
	for range 40 {
		b := randomBoard(rng)
		radius, viewers := rng.IntN(9), rng.IntN(6)+1
		f, err := NewField(b.w, b.h, viewers, radius, func(x, y int) bool {
			if x < 0 || y < 0 || x >= b.w || y >= b.h {
				t.Fatalf("blocked was called for (%d, %d), off the grid", x, y)
			}
			return b.walls[y*b.w+x]
		})
		if err != nil {
			t.Fatal(err)
		}
		at := make([]*grid.Cell, viewers)
		for range 200 {
			v := rng.IntN(viewers)
			switch rng.IntN(5) {
			case 0:
				f.Remove(v)
				at[v] = nil
			case 1:
				f.Move(v, grid.Cell{X: -1, Y: 0})
				at[v] = nil
			case 2:
				if rng.IntN(10) == 0 {
					f.Reset()
					clear(at)
				}
			default:
				c := grid.Cell{X: rng.IntN(b.w), Y: rng.IntN(b.h)}
				changed := f.Move(v, c)
				if changed != (at[v] == nil || *at[v] != c) {
					t.Fatalf("Move to %v reported a change %v from %v", c, changed, at[v])
				}
				at[v] = &c
			}
			want := make([]int32, b.w*b.h)
			for v, c := range at {
				var cells []uint16
				if c != nil {
					Cast(*c, radius, b.blocked, func(x, y int) {
						if x >= 0 && y >= 0 && x < b.w && y < b.h {
							want[y*b.w+x]++
							cells = append(cells, uint16(y*b.w+x)) // #nosec G115 -- small.
						}
					})
				}
				if !slices.Equal(f.Cells(v), cells) {
					t.Fatalf("viewer %d sees %v, want %v", v, f.Cells(v), cells)
				}
			}
			if !slices.Equal(f.count, want) {
				t.Fatalf("counts %v, want %v", f.count, want)
			}
			for i, n := range want {
				if f.Visible(i%b.w, i/b.w) != (n > 0) {
					t.Fatalf("Visible(%d, %d) disagrees with its count %d", i%b.w, i/b.w, n)
				}
			}
			if f.Visible(-1, 0) || f.Visible(b.w, 0) {
				t.Fatal("a cell off the grid is visible")
			}
		}
	}
}

func TestFieldDoesNotAllocate(t *testing.T) {
	const side = 64
	f, err := NewField(side, side, 256, 7, func(x, y int) bool { return (x*7+y*3)%11 == 0 })
	if err != nil {
		t.Fatal(err)
	}
	i := 0
	if allocs := testing.AllocsPerRun(100, func() {
		for v := range 256 {
			f.Move(v, grid.Cell{X: (v*5 + i) % side, Y: (v*3 + i) % side})
		}
		f.Remove(i % 256)
		i++
	}); allocs != 0 {
		t.Fatalf("Move and Remove allocated %v times", allocs)
	}
}

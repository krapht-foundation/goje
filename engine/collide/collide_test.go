package collide_test

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
)

// units builds a rectangle from raw 1/256-tile units.
func units(x0, y0, x1, y1 fixed.Num) fixed.Rect {
	return fixed.Rect{Min: fixed.Vec2{X: x0, Y: y0}, Max: fixed.Vec2{X: x1, Y: y1}}
}

// tiles builds a rectangle from whole tiles.
func tiles(x0, y0, x1, y1 int) fixed.Rect {
	return units(fixed.FromInt(x0), fixed.FromInt(y0), fixed.FromInt(x1), fixed.FromInt(y1))
}

var ground = collide.Interval{Bottom: 0, Top: 20}

func solid(id uint64, r fixed.Rect) collide.Shape {
	return collide.Shape{ID: id, Footprint: r, Vertical: collide.Interval{Bottom: 0, Top: 32}}
}

func body(r fixed.Rect) collide.Shape {
	return collide.Shape{ID: 99, Footprint: r, Vertical: ground}
}

func mustWorld(t *testing.T, width, height int, solids, triggers []collide.Shape) *collide.World {
	t.Helper()
	world, err := collide.New(width, height, solids, triggers)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return world
}

func TestMoveStopsFlushAndSlidesAlongAWall(t *testing.T) {
	t.Parallel()

	// A wall fills x in [4, 5) for the whole board height.
	world := mustWorld(t, 8, 8, []collide.Shape{solid(7, tiles(4, 0, 5, 8))}, nil)
	start := body(tiles(1, 1, 2, 2))

	result, err := world.Move(start, fixed.FromInt(5), fixed.FromInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if want := tiles(3, 3, 4, 4); result.Body.Footprint != want {
		t.Errorf("footprint = %v, want %v: flush on X and full slide on Y", result.Body.Footprint, want)
	}
	if want := (collide.Contact{Blocked: true, Solid: 7}); result.X != want {
		t.Errorf("X contact = %+v, want %+v", result.X, want)
	}
	if result.Y != (collide.Contact{}) {
		t.Errorf("Y contact = %+v, want none", result.Y)
	}

	// Flush against the wall is not an overlap, so the body can leave and
	// move along it again.
	result, err = world.Move(result.Body, fixed.FromInt(-1), fixed.FromInt(1))
	if err != nil {
		t.Fatalf("move from a flush position: %v", err)
	}
	if want := tiles(2, 4, 3, 5); result.Body.Footprint != want {
		t.Errorf("footprint = %v, want %v", result.Body.Footprint, want)
	}
}

func TestMoveLandingExactlyFlushIsNotAContact(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, []collide.Shape{solid(1, tiles(4, 0, 5, 8))}, nil)
	result, err := world.Move(body(tiles(1, 1, 2, 2)), fixed.FromInt(2), 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved.X != fixed.FromInt(2) || result.X.Blocked {
		t.Errorf(
			"moved %d with contact %+v, want a full move without contact",
			result.Moved.X,
			result.X,
		)
	}
}

func TestMoveCannotTunnelThroughAThinSolid(t *testing.T) {
	t.Parallel()

	// A one-unit wall and a step of nearly the whole board.
	world := mustWorld(t, 16, 4, []collide.Shape{solid(1, units(2048, 0, 2049, 1024))}, nil)
	result, err := world.Move(body(tiles(1, 1, 2, 2)), fixed.FromInt(14), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Body.Footprint.Max.X, fixed.Num(2048); got != want {
		t.Errorf("leading edge = %d, want %d", got, want)
	}
	if !result.X.Blocked || result.X.Solid != 1 {
		t.Errorf("X contact = %+v, want the thin wall", result.X)
	}
}

func TestMoveStopsAtInteriorCornerOnBothAxes(t *testing.T) {
	t.Parallel()

	// An L-shaped corner: a wall to the east and one to the south.
	world := mustWorld(t, 8, 8, []collide.Shape{
		solid(1, tiles(4, 0, 5, 8)),
		solid(2, tiles(0, 4, 8, 5)),
	}, nil)
	result, err := world.Move(body(tiles(1, 1, 2, 2)), fixed.FromInt(9), fixed.FromInt(9))
	if err != nil {
		t.Fatal(err)
	}
	if want := tiles(3, 3, 4, 4); result.Body.Footprint != want {
		t.Errorf("footprint = %v, want %v", result.Body.Footprint, want)
	}
	if result.X.Solid != 1 || result.Y.Solid != 2 {
		t.Errorf("contacts X=%+v Y=%+v, want solids 1 and 2", result.X, result.Y)
	}
}

func TestMoveStopsAtBoardEdgeOrTheNearerSolid(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, []collide.Shape{solid(5, tiles(8-1, 0, 8, 8))}, nil)
	// The wall fills the last column, so it stops X before the edge does.
	result, err := world.Move(body(tiles(1, 1, 2, 2)), fixed.FromInt(20), fixed.FromInt(-20))
	if err != nil {
		t.Fatal(err)
	}
	if result.X != (collide.Contact{Blocked: true, Solid: 5}) {
		t.Errorf("X contact = %+v, want the wall, which is nearer than the edge", result.X)
	}
	if result.Y != (collide.Contact{Blocked: true, Edge: true}) {
		t.Errorf("Y contact = %+v, want the board edge", result.Y)
	}
	if want := tiles(6, 0, 7, 1); result.Body.Footprint != want {
		t.Errorf("footprint = %v, want %v", result.Body.Footprint, want)
	}
}

func TestMoveBreaksTiesByLowestID(t *testing.T) {
	t.Parallel()

	// Two solids at the same distance, listed in descending ID order.
	world := mustWorld(t, 8, 8, []collide.Shape{
		solid(9, tiles(4, 2, 5, 3)),
		solid(3, tiles(4, 1, 5, 2)),
	}, nil)
	result, err := world.Move(body(tiles(1, 1, 2, 3)), fixed.FromInt(5), 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.X.Solid != 3 {
		t.Errorf("blocked by solid %d, want 3", result.X.Solid)
	}
}

func TestVerticalExtentDecidesWhatBlocks(t *testing.T) {
	t.Parallel()

	bridge := collide.Shape{
		ID:        1,
		Footprint: tiles(4, 0, 5, 8),
		Vertical:  collide.Interval{Bottom: 24, Top: 32},
	}
	lowWall := collide.Shape{
		ID:        2,
		Footprint: tiles(4, 0, 5, 8),
		Vertical:  collide.Interval{Bottom: 0, Top: 10},
	}

	under := mustWorld(t, 8, 8, []collide.Shape{bridge}, nil)
	result, err := under.Move(body(tiles(1, 1, 2, 2)), fixed.FromInt(5), 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.X.Blocked || result.Body.Footprint.Min.X != fixed.FromInt(6) {
		t.Errorf(
			"under the bridge: footprint %v, contact %+v; want a clear pass",
			result.Body.Footprint,
			result.X,
		)
	}

	// A body whose top sits exactly at the bridge's bottom still passes.
	tall := body(tiles(1, 1, 2, 2))
	tall.Vertical = collide.Interval{Bottom: 4, Top: 24}
	if result, err = under.Move(tall, fixed.FromInt(5), 0); err != nil || result.X.Blocked {
		t.Errorf("touching vertical extents blocked: contact %+v, err %v", result.X, err)
	}

	// A body standing on the deck is blocked by the deck and not by the low wall.
	onDeck := body(tiles(1, 1, 2, 2))
	onDeck.Vertical = collide.Interval{Bottom: 24, Top: 40}
	both := mustWorld(t, 8, 8, []collide.Shape{bridge, lowWall}, nil)
	if result, err = both.Move(onDeck, fixed.FromInt(5), 0); err != nil || result.X.Solid != 1 {
		t.Errorf("on the deck: contact %+v, err %v; want solid 1", result.X, err)
	}

	blocked := mustWorld(t, 8, 8, []collide.Shape{lowWall}, nil)
	if result, err = blocked.Move(
		body(tiles(1, 1, 2, 2)),
		fixed.FromInt(5),
		0,
	); err != nil ||
		result.X.Solid != 2 {
		t.Errorf("low wall: contact %+v, err %v; want solid 2", result.X, err)
	}
}

func TestMoveRejectsBodiesThatStartInvalid(t *testing.T) {
	t.Parallel()

	world := mustWorld(
		t,
		8,
		8,
		[]collide.Shape{solid(4, tiles(3, 3, 5, 5)), solid(2, tiles(3, 3, 5, 5))},
		nil,
	)
	_, err := world.Move(body(tiles(4, 4, 5, 5)), fixed.One, 0)
	if !errors.Is(err, collide.ErrEmbedded) {
		t.Errorf("embedded body: err = %v, want ErrEmbedded", err)
	}
	if _, err = world.Move(
		body(tiles(7, 1, 9, 2)),
		fixed.One,
		0,
	); !errors.Is(
		err,
		collide.ErrOutside,
	) {
		t.Errorf("body past the edge: err = %v, want ErrOutside", err)
	}
	if _, err = world.Move(
		body(tiles(1, 1, 1, 2)),
		fixed.One,
		0,
	); !errors.Is(
		err,
		collide.ErrInvalid,
	) {
		t.Errorf("empty body: err = %v, want ErrInvalid", err)
	}
}

func TestNewRejectsMalformedWorlds(t *testing.T) {
	t.Parallel()

	good := solid(1, tiles(1, 1, 2, 2))
	for name, build := range map[string]func() (*collide.World, error){
		"zero width": func() (*collide.World, error) { return collide.New(0, 4, nil, nil) },
		"too tall":   func() (*collide.World, error) { return collide.New(4, 129, nil, nil) },
		"empty solid": func() (*collide.World, error) {
			return collide.New(4, 4, []collide.Shape{solid(1, tiles(1, 1, 1, 2))}, nil)
		},
		"outside": func() (*collide.World, error) {
			return collide.New(4, 4, []collide.Shape{solid(1, tiles(3, 3, 5, 4))}, nil)
		},
		"negative": func() (*collide.World, error) {
			return collide.New(4, 4, []collide.Shape{solid(1, tiles(-1, 0, 1, 1))}, nil)
		},
		"no height": func() (*collide.World, error) {
			return collide.New(4, 4, []collide.Shape{{ID: 1, Footprint: good.Footprint}}, nil)
		},
		"duplicate":      func() (*collide.World, error) { return collide.New(4, 4, []collide.Shape{good, good}, nil) },
		"duplicate trig": func() (*collide.World, error) { return collide.New(4, 4, nil, []collide.Shape{good, good}) },
	} {
		if _, err := build(); !errors.Is(err, collide.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	// A solid and a trigger may share an ID: the lists are separate.
	if _, err := collide.New(4, 4, []collide.Shape{good}, []collide.Shape{good}); err != nil {
		t.Errorf("shared ID across lists: %v", err)
	}
}

func TestOverlapsListsSolidsThenTriggersByID(t *testing.T) {
	t.Parallel()

	world := mustWorld(
		t,
		8,
		8,
		[]collide.Shape{
			solid(8, tiles(1, 1, 5, 5)),
			solid(2, tiles(2, 2, 3, 3)),
			solid(5, tiles(6, 6, 7, 7)),
		},
		[]collide.Shape{solid(4, tiles(0, 0, 8, 8)), solid(1, tiles(2, 2, 4, 4))},
	)
	got := world.Overlaps(nil, tiles(2, 2, 4, 4), ground)
	want := []collide.Hit{{ID: 2}, {ID: 8}, {ID: 1, Trigger: true}, {ID: 4, Trigger: true}}
	if !slices.Equal(got, want) {
		t.Errorf("Overlaps = %v, want %v", got, want)
	}

	// Touching edges, empty rectangles, and non-overlapping heights hit nothing.
	if got = world.Overlaps(nil, tiles(5, 5, 6, 6), ground); len(got) != 1 || got[0].ID != 4 {
		t.Errorf("touching region hits = %v, want only the board-wide trigger", got)
	}
	if got = world.Overlaps(nil, tiles(2, 2, 2, 4), ground); len(got) != 0 {
		t.Errorf("empty rectangle hits = %v", got)
	}
	if got = world.Overlaps(
		nil,
		tiles(2, 2, 4, 4),
		collide.Interval{Bottom: 32, Top: 40},
	); len(
		got,
	) != 0 {
		t.Errorf("height above everything hits = %v", got)
	}
	// A query that extends past the board still finds shapes inside it.
	if got = world.Overlaps(nil, tiles(-3, -3, 20, 20), ground); len(got) != 5 {
		t.Errorf("oversized query hits %d shapes, want 5", len(got))
	}
}

// randomShapes returns n shapes with distinct IDs inside a 6×6 tile board.
// Sizes are uneven so that shapes span several tiles and touch tile edges.
func randomShapes(rng *rand.Rand, n int) []collide.Shape {
	shapes := make([]collide.Shape, n)
	for i := range shapes {
		x0, y0 := fixed.Num(rng.IntN(6*256-1)), fixed.Num(rng.IntN(6*256-1))
		x1 := min(x0+1+fixed.Num(rng.IntN(300)), 6*256)
		y1 := min(y0+1+fixed.Num(rng.IntN(300)), 6*256)
		bottom := int32(rng.IntN(40))
		shapes[i] = collide.Shape{
			ID:        1 + uint64(rng.IntN(1000))*uint64(n) + uint64(i),
			Footprint: units(x0, y0, x1, y1),
			Vertical:  collide.Interval{Bottom: bottom, Top: bottom + 1 + int32(rng.IntN(30))},
		}
	}
	return shapes
}

// referenceOverlaps scans every shape instead of using the index.
func referenceOverlaps(
	solids, triggers []collide.Shape,
	r fixed.Rect,
	v collide.Interval,
) []collide.Hit {
	var hits []collide.Hit
	for kind, list := range [][]collide.Shape{solids, triggers} {
		start := len(hits)
		for _, s := range list {
			if s.Footprint.Overlaps(r) && s.Vertical.Overlaps(v) {
				hits = append(hits, collide.Hit{ID: s.ID, Trigger: kind == 1})
			}
		}
		slices.SortFunc(hits[start:], func(a, b collide.Hit) int { return int(a.ID) - int(b.ID) })
	}
	return hits
}

func TestOverlapsMatchesAScanOfEveryShape(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2))
	for round := range 40 {
		solids := randomShapes(rng, 1+rng.IntN(25))
		triggers := randomShapes(rng, 1+rng.IntN(8))
		world := mustWorld(t, 6, 6, solids, triggers)
		for range 60 {
			x0, y0 := fixed.Num(rng.IntN(7*256))-256, fixed.Num(rng.IntN(7*256))-256
			q := units(x0, y0, x0+fixed.Num(rng.IntN(500)), y0+fixed.Num(rng.IntN(500)))
			bottom := int32(rng.IntN(40))
			v := collide.Interval{Bottom: bottom, Top: bottom + int32(rng.IntN(20))}
			got := world.Overlaps(nil, q, v)
			want := referenceOverlaps(solids, triggers, q, v)
			if !slices.Equal(got, want) {
				t.Fatalf("round %d: Overlaps(%v, %v) = %v, want %v", round, q, v, got, want)
			}
		}
	}
}

// referenceMove advances one unit at a time and tests every solid, so it shares
// no sweep logic with World.Move. It is slow and exact.
func referenceMove(
	solids []collide.Shape,
	b collide.Shape,
	dx, dy fixed.Num,
) (fixed.Rect, [2]collide.Contact) {
	var contacts [2]collide.Contact
	const board = 6 * 256
	for axis, d := range [2]fixed.Num{dx, dy} {
		step := fixed.Vec2{}
		unit := fixed.Num(1)
		if d < 0 {
			unit = -1
		}
		if axis == 0 {
			step.X = unit
		} else {
			step.Y = unit
		}
		for range d.Abs() {
			next := b.Footprint.Translate(step)
			blocker, blocked := uint64(0), false
			for _, s := range solids {
				if s.Footprint.Overlaps(next) && s.Vertical.Overlaps(b.Vertical) &&
					(!blocked || s.ID < blocker) {
					blocker, blocked = s.ID, true
				}
			}
			outside := next.Min.X < 0 || next.Min.Y < 0 || next.Max.X > board || next.Max.Y > board
			if blocked {
				contacts[axis] = collide.Contact{Blocked: true, Solid: blocker}
				break
			}
			if outside {
				contacts[axis] = collide.Contact{Blocked: true, Edge: true}
				break
			}
			b.Footprint = next
		}
	}
	return b.Footprint, contacts
}

func TestMoveMatchesUnitStepReference(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4))
	for round := range 60 {
		solids := randomShapes(rng, 1+rng.IntN(20))
		world := mustWorld(t, 6, 6, solids, nil)
		for range 40 {
			x0, y0 := fixed.Num(rng.IntN(5*256)), fixed.Num(rng.IntN(5*256))
			b := collide.Shape{
				ID:        99,
				Footprint: units(x0, y0, x0+1+fixed.Num(rng.IntN(150)), y0+1+fixed.Num(rng.IntN(150))),
				Vertical:  collide.Interval{Bottom: int32(rng.IntN(30)), Top: 31 + int32(rng.IntN(20))},
			}
			dx, dy := fixed.Num(rng.IntN(1500)-750), fixed.Num(rng.IntN(1500)-750)

			result, err := world.Move(b, dx, dy)
			if len(referenceOverlaps(solids, nil, b.Footprint, b.Vertical)) > 0 {
				if !errors.Is(err, collide.ErrEmbedded) {
					t.Fatalf("round %d: embedded body: err = %v, want ErrEmbedded", round, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("round %d: Move: %v", round, err)
			}
			footprint, contacts := referenceMove(solids, b, dx, dy)
			if result.Body.Footprint != footprint || result.X != contacts[0] || result.Y != contacts[1] {
				t.Fatalf("round %d: Move(%v, %d, %d) = %v X=%+v Y=%+v; reference %v X=%+v Y=%+v",
					round, b.Footprint, dx, dy, result.Body.Footprint, result.X, result.Y,
					footprint, contacts[0], contacts[1])
			}
			if got := result.Body.Footprint.Min.Sub(b.Footprint.Min); got != result.Moved {
				t.Fatalf("round %d: Moved = %v, want %v", round, result.Moved, got)
			}
		}
	}
}

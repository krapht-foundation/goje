package collide_test

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
)

func mustBodies(t *testing.T, width, height int, shapes ...collide.Shape) *collide.Bodies {
	t.Helper()
	bodies, err := collide.NewBodies(width, height)
	if err != nil {
		t.Fatalf("NewBodies: %v", err)
	}
	for _, s := range shapes {
		if err := bodies.Set(s); err != nil {
			t.Fatalf("Set(%d): %v", s.ID, err)
		}
	}
	return bodies
}

func unit(id uint64, r fixed.Rect) collide.Shape {
	return collide.Shape{ID: id, Footprint: r, Vertical: ground}
}

func TestBodiesSetReplacesAndRemovesByID(t *testing.T) {
	t.Parallel()

	bodies := mustBodies(t, 8, 8, unit(1, tiles(1, 1, 2, 2)), unit(2, tiles(5, 5, 6, 6)))
	if bodies.Len() != 2 {
		t.Fatalf("Len = %d, want 2", bodies.Len())
	}
	// Replacing moves the body across tiles, so a query on the old place no
	// longer sees it.
	if err := bodies.Set(unit(1, tiles(3, 3, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if got := bodies.Overlaps(nil, tiles(1, 1, 2, 2), ground); len(got) != 0 {
		t.Errorf("old place still lists %v", got)
	}
	if got := bodies.Overlaps(nil, tiles(3, 3, 4, 4), ground); !slices.Equal(got, []uint64{1}) {
		t.Errorf("new place lists %v, want [1]", got)
	}
	if got, ok := bodies.Get(1); !ok || got.Footprint != tiles(3, 3, 4, 4) {
		t.Errorf("Get(1) = %v, %v", got, ok)
	}
	if !bodies.Remove(1) || bodies.Remove(1) {
		t.Error("Remove must report true once and false after")
	}
	if _, ok := bodies.Get(1); ok || bodies.Len() != 1 {
		t.Error("removed body is still present")
	}
	// A freed slot is reused without disturbing the other body.
	if err := bodies.Set(unit(3, tiles(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if got := bodies.Overlaps(nil, tiles(0, 0, 8, 8), ground); !slices.Equal(got, []uint64{2, 3}) {
		t.Errorf("all bodies = %v, want [2 3]", got)
	}
}

func TestBodiesSetRejectsInvalidShapesAndKeepsTheSet(t *testing.T) {
	t.Parallel()

	bodies := mustBodies(t, 4, 4, unit(1, tiles(1, 1, 2, 2)))
	tests := []struct {
		name string
		s    collide.Shape
		want error
	}{
		{"zero ID", unit(0, tiles(0, 0, 1, 1)), collide.ErrInvalid},
		{"empty footprint", unit(2, tiles(1, 1, 1, 2)), collide.ErrInvalid},
		{"empty vertical", collide.Shape{ID: 2, Footprint: tiles(0, 0, 1, 1)}, collide.ErrInvalid},
		{"outside", unit(2, tiles(3, 3, 5, 4)), collide.ErrOutside},
		{"replacement outside", unit(1, tiles(-1, 0, 1, 1)), collide.ErrOutside},
	}
	for _, tt := range tests {
		if err := bodies.Set(tt.s); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	if got, _ := bodies.Get(1); got.Footprint != tiles(1, 1, 2, 2) || bodies.Len() != 1 {
		t.Errorf("a rejected Set changed the set: %v, len %d", got, bodies.Len())
	}
	if _, err := collide.NewBodies(0, 4); !errors.Is(err, collide.ErrInvalid) {
		t.Errorf("NewBodies(0, 4) err = %v, want ErrInvalid", err)
	}
}

func TestBodiesHoldAtMostMaxBodies(t *testing.T) {
	t.Parallel()

	bodies := mustBodies(t, 128, 128)
	for i := range collide.MaxBodies {
		x, y := i%128, i/128
		if err := bodies.Set(unit(uint64(i+1), tiles(x, y, x+1, y+1))); err != nil {
			t.Fatalf("body %d: %v", i, err)
		}
	}
	if err := bodies.Set(
		unit(collide.MaxBodies+1, tiles(0, 100, 1, 101)),
	); !errors.Is(
		err,
		collide.ErrInvalid,
	) {
		t.Errorf("one past the limit: err = %v, want ErrInvalid", err)
	}
	// Replacing an existing body is not an insertion, so it still works.
	if err := bodies.Set(unit(1, tiles(0, 100, 1, 101))); err != nil {
		t.Errorf("replace at the limit: %v", err)
	}
}

func TestBodiesOverlapsMatchesAScanOfEveryBody(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(5, 6))
	for round := range 40 {
		shapes := randomShapes(rng, 1+rng.IntN(25))
		bodies := mustBodies(t, 6, 6, shapes...)
		// Move some bodies so that tile membership changes are exercised.
		for range 10 {
			s := shapes[rng.IntN(len(shapes))]
			if rng.IntN(3) == 0 {
				bodies.Remove(s.ID)
				continue
			}
			x0, y0 := fixed.Num(rng.IntN(5*256)), fixed.Num(rng.IntN(5*256))
			s.Footprint = units(x0, y0, x0+1+fixed.Num(rng.IntN(300)), y0+1+fixed.Num(rng.IntN(300)))
			s.Footprint.Max.X, s.Footprint.Max.Y = min(
				s.Footprint.Max.X,
				6*256,
			), min(
				s.Footprint.Max.Y,
				6*256,
			)
			if err := bodies.Set(s); err != nil {
				t.Fatal(err)
			}
		}
		var current []collide.Shape
		for _, s := range shapes {
			if got, ok := bodies.Get(s.ID); ok {
				current = append(current, got)
			}
		}
		for range 60 {
			x0, y0 := fixed.Num(rng.IntN(7*256))-256, fixed.Num(rng.IntN(7*256))-256
			q := units(x0, y0, x0+fixed.Num(rng.IntN(500)), y0+fixed.Num(rng.IntN(500)))
			bottom := int32(rng.IntN(40))
			v := collide.Interval{Bottom: bottom, Top: bottom + int32(rng.IntN(20))}
			hits := referenceOverlaps(current, nil, q, v)
			want := make([]uint64, 0, len(hits))
			for _, h := range hits {
				want = append(want, h.ID)
			}
			if got := bodies.Overlaps(nil, q, v); !slices.Equal(got, want) {
				t.Fatalf("round %d: Overlaps(%v, %v) = %v, want %v", round, q, v, got, want)
			}
		}
	}
}

func TestMoveAmongStopsFlushAgainstABodyAndIgnoresItself(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, nil, nil)
	mover := unit(1, tiles(1, 1, 2, 2))
	bodies := mustBodies(t, 8, 8, mover, unit(2, tiles(4, 1, 5, 2)))

	result, err := world.MoveAmong(bodies, mover, fixed.FromInt(5), fixed.FromInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if want := tiles(3, 2, 4, 3); result.Body.Footprint != want {
		t.Errorf("footprint = %v, want %v: flush on X, full slide on Y", result.Body.Footprint, want)
	}
	if want := (collide.Contact{Blocked: true, Body: 2}); result.X != want {
		t.Errorf("X contact = %+v, want %+v", result.X, want)
	}
	// MoveAmong does not store the result.
	if got, _ := bodies.Get(1); got.Footprint != mover.Footprint {
		t.Errorf("stored body moved to %v", got.Footprint)
	}
	// Without the other body the same move is free, so the block was the body.
	if free, err := world.MoveAmong(nil, mover, fixed.FromInt(5), 0); err != nil || free.X.Blocked {
		t.Errorf("nil set: %+v, %v", free.X, err)
	}
}

func TestMoveAmongTiesPreferSolidsThenTheLowestBodyID(t *testing.T) {
	t.Parallel()

	mover := unit(9, tiles(1, 1, 2, 2))
	world := mustWorld(t, 8, 8, []collide.Shape{solid(40, tiles(4, 0, 5, 8))}, nil)
	bodies := mustBodies(t, 8, 8, unit(7, tiles(4, 1, 5, 2)), unit(3, tiles(4, 1, 5, 2)))
	result, err := world.MoveAmong(bodies, mover, fixed.FromInt(5), 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := (collide.Contact{Blocked: true, Solid: 40}); result.X != want {
		t.Errorf("solid and bodies tie: %+v, want %+v", result.X, want)
	}

	world = mustWorld(t, 8, 8, nil, nil)
	if result, err = world.MoveAmong(bodies, mover, fixed.FromInt(5), 0); err != nil {
		t.Fatal(err)
	}
	if want := (collide.Contact{Blocked: true, Body: 3}); result.X != want {
		t.Errorf("two bodies tie: %+v, want %+v", result.X, want)
	}
}

func TestMoveAmongBodiesBlockOnlyAtOverlappingHeights(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, nil, nil)
	flier := collide.Shape{
		ID:        2,
		Footprint: tiles(3, 1, 4, 2),
		Vertical:  collide.Interval{Bottom: 40, Top: 60},
	}
	bodies := mustBodies(t, 8, 8, flier)
	result, err := world.MoveAmong(bodies, unit(1, tiles(1, 1, 2, 2)), fixed.FromInt(5), 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.X.Blocked {
		t.Errorf("a body at another height blocked the move: %+v", result.X)
	}
}

func TestMoveAmongRejectsABodyThatStartsOnAnotherBody(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, nil, nil)
	bodies := mustBodies(t, 8, 8, unit(2, tiles(1, 1, 3, 3)), unit(5, tiles(2, 2, 4, 4)))
	_, err := world.MoveAmong(bodies, unit(1, tiles(2, 2, 3, 3)), fixed.One, 0)
	if !errors.Is(err, collide.ErrEmbedded) {
		t.Fatalf("err = %v, want ErrEmbedded", err)
	}
	// The error names the lowest overlapped body.
	if want := "body 1 and body 2"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
}

// referenceMoveAmong advances one unit at a time against every solid and every
// other body. It shares no sweep logic with World.MoveAmong.
func referenceMoveAmong(
	solids, bodies []collide.Shape,
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
			solidID, solidHit := uint64(0), false
			for _, s := range solids {
				if s.Footprint.Overlaps(next) && s.Vertical.Overlaps(b.Vertical) &&
					(!solidHit || s.ID < solidID) {
					solidID, solidHit = s.ID, true
				}
			}
			bodyID, bodyHit := uint64(0), false
			for _, s := range bodies {
				if s.ID != b.ID && s.Footprint.Overlaps(next) && s.Vertical.Overlaps(b.Vertical) &&
					(!bodyHit || s.ID < bodyID) {
					bodyID, bodyHit = s.ID, true
				}
			}
			outside := next.Min.X < 0 || next.Min.Y < 0 || next.Max.X > board || next.Max.Y > board
			switch {
			case solidHit:
				contacts[axis] = collide.Contact{Blocked: true, Solid: solidID}
			case outside:
				contacts[axis] = collide.Contact{Blocked: true, Edge: true}
			case bodyHit:
				contacts[axis] = collide.Contact{Blocked: true, Body: bodyID}
			}
			if contacts[axis].Blocked {
				break
			}
			b.Footprint = next
		}
	}
	return b.Footprint, contacts
}

func TestMoveAmongMatchesUnitStepReference(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(7, 8))
	for round := range 60 {
		solids := randomShapes(rng, 1+rng.IntN(12))
		others := randomShapes(rng, 1+rng.IntN(12))
		world := mustWorld(t, 6, 6, solids, nil)
		bodies := mustBodies(t, 6, 6, others...)
		for range 40 {
			x0, y0 := fixed.Num(rng.IntN(5*256)), fixed.Num(rng.IntN(5*256))
			b := collide.Shape{
				ID:        99,
				Footprint: units(x0, y0, x0+1+fixed.Num(rng.IntN(150)), y0+1+fixed.Num(rng.IntN(150))),
				Vertical:  collide.Interval{Bottom: int32(rng.IntN(30)), Top: 31 + int32(rng.IntN(20))},
			}
			dx, dy := fixed.Num(rng.IntN(1500)-750), fixed.Num(rng.IntN(1500)-750)

			result, err := world.MoveAmong(bodies, b, dx, dy)
			embedded := len(referenceOverlaps(solids, nil, b.Footprint, b.Vertical)) > 0
			for _, s := range others {
				embedded = embedded ||
					s.ID != b.ID && s.Footprint.Overlaps(b.Footprint) && s.Vertical.Overlaps(b.Vertical)
			}
			if embedded {
				if !errors.Is(err, collide.ErrEmbedded) {
					t.Fatalf("round %d: embedded body: err = %v, want ErrEmbedded", round, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("round %d: MoveAmong: %v", round, err)
			}
			footprint, contacts := referenceMoveAmong(solids, others, b, dx, dy)
			if result.Body.Footprint != footprint || result.X != contacts[0] || result.Y != contacts[1] {
				t.Fatalf("round %d: MoveAmong(%v, %d, %d) = %v X=%+v Y=%+v; reference %v X=%+v Y=%+v",
					round, b.Footprint, dx, dy, result.Body.Footprint, result.X, result.Y,
					footprint, contacts[0], contacts[1])
			}
		}
	}
}

func TestBodiesSteadyStateAllocatesNothing(t *testing.T) {
	world := mustWorld(t, 16, 16, []collide.Shape{solid(1, tiles(8, 0, 9, 16))}, nil)
	bodies := mustBodies(t, 16, 16)
	for i := range 40 {
		x, y := i%8, i/8*2
		if err := bodies.Set(unit(uint64(i+1), tiles(x, y, x+1, y+1))); err != nil {
			t.Fatal(err)
		}
	}
	hits := make([]uint64, 0, 64)
	ticks := 0
	step := func() {
		// Walk right for 20 ticks, then left, so bodies keep crossing tile edges.
		dx := fixed.Num(30)
		if ticks/20%2 == 1 {
			dx = -30
		}
		ticks++
		for id := uint64(1); id <= 40; id++ {
			b, _ := bodies.Get(id)
			result, err := world.MoveAmong(bodies, b, dx, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := bodies.Set(result.Body); err != nil {
				t.Fatal(err)
			}
		}
		hits = bodies.Overlaps(hits[:0], tiles(0, 0, 16, 16), ground)
	}
	// Warm up so tile lists have grown to fit the bodies that cross them.
	for range 200 {
		step()
	}
	if allocs := testing.AllocsPerRun(50, step); allocs != 0 {
		t.Errorf("steady-state moves allocated %v times, want 0", allocs)
	}
}

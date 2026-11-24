package collide_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/level"
)

const authoredLevel = `{"version":2,"rows":["......",".#....","......"],"placements":[],"zones":[` +
	`{"id":5,"kind":"deck","role":"solid","x":3,"y":0,"w":2,"h":3,"bottom":24,"top":32},` +
	`{"id":6,"kind":"door","role":"trigger","x":0,"y":2,"w":1,"h":1,"bottom":0,"top":20}]}`

func TestFromLevelBuildsWallsAndZones(t *testing.T) {
	t.Parallel()

	board, err := level.Decode(strings.NewReader(authoredLevel))
	if err != nil {
		t.Fatal(err)
	}
	world, err := collide.FromLevel(board, collide.Interval{Bottom: 0, Top: 32})
	if err != nil {
		t.Fatal(err)
	}

	// The wall at (1, 1) blocks a body at ground height and carries a derived ID.
	hits := world.Overlaps(nil, tiles(1, 1, 2, 2), ground)
	want := []collide.Hit{{ID: collide.WallID(6, 1, 1)}}
	if !slices.Equal(hits, want) {
		t.Errorf("wall cell overlaps = %+v, want %+v", hits, want)
	}
	if collide.WallID(6, 1, 1) < collide.WallIDBase {
		t.Error("a wall ID must be above every zone ID")
	}

	// The deck zone stops nothing at ground height but blocks above it.
	if got := world.Overlaps(nil, tiles(3, 0, 5, 3), ground); len(got) != 0 {
		t.Errorf("deck blocks ground height: %+v", got)
	}
	if got := world.Overlaps(
		nil,
		tiles(3, 0, 5, 3),
		collide.Interval{Bottom: 24, Top: 30},
	); !slices.Equal(
		got,
		[]collide.Hit{{ID: 5}},
	) {
		t.Errorf("deck overlaps = %+v, want zone 5", got)
	}
	// The doorway is a trigger: it is reported, and it never blocks a move.
	if got := world.Overlaps(
		nil,
		tiles(0, 2, 1, 3),
		ground,
	); !slices.Equal(
		got,
		[]collide.Hit{{ID: 6, Trigger: true}},
	) {
		t.Errorf("door overlaps = %+v, want trigger 6", got)
	}
	result, err := world.Move(body(units(8, 2*256+8, 100, 2*256+100)), fixed.FromInt(1), 0)
	if err != nil || result.X.Blocked {
		t.Errorf("a trigger blocked a move: %+v, %v", result.X, err)
	}
	// Walking along the deck's row under it is free, and the wall still stops a body.
	moved, err := world.Move(body(tiles(0, 1, 1, 2)), fixed.FromInt(3), 0)
	if err != nil {
		t.Fatal(err)
	}
	if moved.X.Solid != collide.WallID(6, 1, 1) || moved.Body.Footprint.Max.X != fixed.FromInt(1) {
		t.Errorf("wall contact = %+v, footprint %v", moved.X, moved.Body.Footprint)
	}
}

func TestFromLevelRejectsAnEmptyLevel(t *testing.T) {
	t.Parallel()

	if _, err := collide.FromLevel(level.Board{}, ground); err == nil {
		t.Error("an empty level must not build a world")
	}
}

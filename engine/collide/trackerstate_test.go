package collide_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/collide"
)

func TestTrackerRestoreReportsNoEntryForBodiesAlreadyInside(t *testing.T) {
	t.Parallel()

	world := mustWorld(
		t,
		8,
		8,
		nil,
		[]collide.Shape{solid(10, tiles(0, 0, 4, 4)), solid(20, tiles(2, 2, 6, 6))},
	)
	at := func(id uint64, x, y int) collide.Shape {
		b := body(tiles(x, y, x+1, y+1))
		b.ID = id
		return b
	}
	bodies := []collide.Shape{at(1, 3, 3), at(2, 0, 0)}

	original := collide.NewTracker(world)
	original.Update(nil, bodies)
	saved := original.AppendOverlaps(nil)
	want := []collide.Overlap{{Trigger: 10, Body: 1}, {Trigger: 10, Body: 2}, {Trigger: 20, Body: 1}}
	if !slices.Equal(saved, want) {
		t.Fatalf("overlaps = %v, want %v", saved, want)
	}

	restored := collide.NewTracker(world)
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if got := restored.AppendOverlaps(nil); !slices.Equal(got, want) {
		t.Fatalf("restored overlaps = %v, want %v", got, want)
	}
	// Body 1 stays; body 2 walks out of trigger 10 and body 3 appears inside it.
	moved := []collide.Shape{at(1, 3, 3), at(2, 6, 0), at(3, 1, 1)}
	a := original.Update(nil, moved)
	b := restored.Update(nil, moved)
	wantEvents := []collide.Event{{Trigger: 10, Body: 2}, {Trigger: 10, Body: 3, Enter: true}}
	if !slices.Equal(a, wantEvents) || !slices.Equal(b, wantEvents) {
		t.Fatalf("events after restore = %v and %v, want %v", a, b, wantEvents)
	}
}

func TestTrackerRestoreRejectsBadStateAndKeepsTheOld(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, nil, []collide.Shape{solid(10, tiles(0, 0, 4, 4))})
	tracker := collide.NewTracker(world)
	good := []collide.Overlap{{Trigger: 10, Body: 4}}
	if err := tracker.Restore(good); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]collide.Overlap{
		"unknown trigger": {{Trigger: 11, Body: 1}},
		"duplicate":       {{Trigger: 10, Body: 1}, {Trigger: 10, Body: 1}},
		"descending":      {{Trigger: 10, Body: 2}, {Trigger: 10, Body: 1}},
	} {
		if err := tracker.Restore(bad); !errors.Is(err, collide.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if got := tracker.AppendOverlaps(nil); !slices.Equal(got, good) {
		t.Fatalf("overlaps after rejected restores = %v, want %v", got, good)
	}
	if err := tracker.Restore(nil); err != nil || len(tracker.AppendOverlaps(nil)) != 0 {
		t.Fatalf("restoring no overlaps: err %v, overlaps %v", err, tracker.AppendOverlaps(nil))
	}
}

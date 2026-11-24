package collide_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
)

func TestTrackerReportsEnterThenExitInIDOrder(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, nil, []collide.Shape{
		solid(20, tiles(2, 2, 4, 4)),
		solid(10, tiles(3, 3, 6, 6)),
	})
	tracker := collide.NewTracker(world)
	at := func(id uint64, r fixed.Rect) collide.Shape {
		b := body(r)
		b.ID = id
		return b
	}

	// Body 7 spawns inside both triggers; body 5 spawns inside only trigger 20.
	events := tracker.Update(nil, []collide.Shape{at(7, tiles(3, 3, 4, 4)), at(5, tiles(2, 2, 3, 3))})
	want := []collide.Event{
		{Trigger: 10, Body: 7, Enter: true},
		{Trigger: 20, Body: 5, Enter: true},
		{Trigger: 20, Body: 7, Enter: true},
	}
	if !slices.Equal(events, want) {
		t.Fatalf("first update = %v, want %v", events, want)
	}

	// Nothing changed: no events. Body 5 is omitted, so it exits trigger 20;
	// body 7 moves from both triggers into trigger 10 only.
	if events = tracker.Update(
		events[:0],
		[]collide.Shape{at(7, tiles(3, 3, 4, 4)), at(5, tiles(2, 2, 3, 3))},
	); len(
		events,
	) != 0 {
		t.Fatalf("unchanged update = %v, want none", events)
	}
	events = tracker.Update(events[:0], []collide.Shape{at(7, tiles(4, 4, 5, 5))})
	want = []collide.Event{{Trigger: 20, Body: 5}, {Trigger: 20, Body: 7}}
	if !slices.Equal(events, want) {
		t.Fatalf("exit update = %v, want %v", events, want)
	}
}

func TestTrackerIgnoresTouchingEdgesAndOtherHeights(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 8, 8, nil, []collide.Shape{
		{ID: 1, Footprint: tiles(2, 2, 4, 4), Vertical: collide.Interval{Bottom: 24, Top: 32}},
		{ID: 2, Footprint: tiles(5, 5, 6, 6), Vertical: collide.Interval{Bottom: 0, Top: 32}},
	})
	tracker := collide.NewTracker(world)
	beside := body(tiles(4, 5, 5, 6)) // touches trigger 2's left edge
	under := body(tiles(2, 2, 4, 4))  // below trigger 1
	if events := tracker.Update(nil, []collide.Shape{beside, under}); len(events) != 0 {
		t.Errorf("events = %v, want none", events)
	}
}

func TestTrackerAtTheBoardEdge(t *testing.T) {
	t.Parallel()

	world := mustWorld(t, 4, 4, nil, []collide.Shape{solid(1, tiles(3, 0, 4, 4))})
	tracker := collide.NewTracker(world)
	flush := body(tiles(3, 1, 4, 2))
	if events := tracker.Update(nil, []collide.Shape{flush}); len(events) != 1 || !events[0].Enter {
		t.Errorf("flush against the edge: events = %v, want one entry", events)
	}
	// A body hanging past the board still overlaps the part that is inside.
	pastEdge := body(tiles(3, 1, 6, 2))
	if events := tracker.Update(nil, []collide.Shape{pastEdge}); len(events) != 0 {
		t.Errorf("sliding past the edge: events = %v, want none", events)
	}
}

func TestTrackerMatchesSetDifferenceReference(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(5, 6))
	for round := range 30 {
		triggers := randomShapes(rng, 1+rng.IntN(10))
		world := mustWorld(t, 6, 6, nil, triggers)
		tracker := collide.NewTracker(world)
		bodies := randomShapes(rng, 1+rng.IntN(12))
		previous := map[[2]uint64]bool{}
		var events []collide.Event
		for tick := range 30 {
			// Move the bodies by small random steps, and drop one now and then.
			for i := range bodies {
				step := fixed.Vec2{X: fixed.Num(rng.IntN(200) - 100), Y: fixed.Num(rng.IntN(200) - 100)}
				bodies[i].Footprint = bodies[i].Footprint.Translate(step)
			}
			live := bodies
			if tick%7 == 3 && len(bodies) > 1 {
				live = bodies[1:]
			}
			events = tracker.Update(events[:0], live)

			current := map[[2]uint64]bool{}
			for _, b := range live {
				for _, hit := range world.Overlaps(nil, b.Footprint, b.Vertical) {
					current[[2]uint64{hit.ID, b.ID}] = true
				}
			}
			var want []collide.Event
			for key := range current {
				if !previous[key] {
					want = append(want, collide.Event{Trigger: key[0], Body: key[1], Enter: true})
				}
			}
			for key := range previous {
				if !current[key] {
					want = append(want, collide.Event{Trigger: key[0], Body: key[1]})
				}
			}
			slices.SortFunc(want, func(a, b collide.Event) int {
				if a.Trigger != b.Trigger {
					return int(a.Trigger) - int(b.Trigger)
				}
				return int(a.Body) - int(b.Body)
			})
			if !slices.Equal(events, want) {
				t.Fatalf("round %d tick %d: events = %v, want %v", round, tick, events, want)
			}
			previous = current
		}
	}
}

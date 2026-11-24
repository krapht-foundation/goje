# Move and collide in your game

Goje's collision packages answer one question: can this body move there? They use ground coordinates and exact integer arithmetic, so a game gets the same result on desktop and in the browser. They do not route units, steer crowds, or simulate physics. See the [behavior specification](../specs/collision-m12.md) for the full rules.

## Try it

Run the yard fixture, where the scout walks freely through the courtyard's level:

```sh
GOJE_PLAYGROUND=yard make run-desktop
```

In the browser, start `make serve-web` and open `http://127.0.0.1:8080/?fixture=yard`. Move with the arrow keys or **WASD**, and press **R** to restart. Walk into the wall and keep holding a direction to slide along it. Walk through the gap at the north end to light the doorway lamp. Walk into the southern part of the board to pass beneath the raised deck, which does not block the scout because it sits above it. The magenta outline is the footprint that collision uses.

## Use the packages

Positions are `fixed.Num` values in 1/256 tile, and heights are whole projected pixels, the same unit as `engine.WorldItem.Elevation`. Build the world once for a scene from your own data, then move bodies each tick.

```go
tile := func(x, y int) fixed.Rect {
	return fixed.Rect{
		Min: fixed.Vec2{X: fixed.FromInt(x), Y: fixed.FromInt(y)},
		Max: fixed.Vec2{X: fixed.FromInt(x + 1), Y: fixed.FromInt(y + 1)},
	}
}
wall := collide.Shape{ID: 1, Footprint: tile(4, 1), Vertical: collide.Interval{Bottom: 0, Top: 32}}
door := collide.Shape{ID: 1, Footprint: tile(4, 0), Vertical: collide.Interval{Bottom: 0, Top: 32}}
world, err := collide.New(9, 7, []collide.Shape{wall}, []collide.Shape{door})
if err != nil {
	return err
}
tracker := collide.NewTracker(world)

// Each tick: move, then ask what changed.
result, err := world.Move(scout, dx, dy)
if err != nil {
	return err
}
scout = result.Body
events = tracker.Update(events[:0], []collide.Shape{scout})
```

- A body is blocked only by solids whose vertical extent overlaps its own. Give a bridge a deck that starts above your characters' heads and they walk beneath it.
- `Move` stops the body flush against what blocks it and reports the contact for each axis. A step that lands exactly flush is not blocked.
- Keep per-tick speed in your game. Convert to drawing positions with `Num.Float64` at the end of the tick; never feed floats back into the simulation.
- Reuse the `events` and result buffers between ticks. After they have grown, a tick allocates nothing.
- Build a new `World` and `Tracker` when a scene restarts or its walls change.

Rectangles are the only shape, bodies do not block each other, and shapes cannot change during a tick. The [measurements](collision-performance.md) show the cost of 2,048 moving bodies on both targets.

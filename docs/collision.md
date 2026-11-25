# Move and collide in your game

Goje's collision packages answer one question: can this body move there? They use ground coordinates and exact integer arithmetic, so a game gets the same result on desktop and in the browser. They do not route units, steer crowds, or simulate physics. A set of moving bodies can block each other, and a world can be built from a level you drew in the Level Builder. See the [behavior specification](../specs/collision-m12.md) for the full rules.

## Try it

Run the yard fixture, where the scout walks freely through a version 2 level with a deck and a doorway:

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

## Block other bodies

A `World` never changes, so moving bodies live in a separate `collide.Bodies` set. Give each body a nonzero ID, `Set` it once, and move it with `World.MoveAmong`, which stops it flush against walls and against the other bodies. It does not store the result, so call `Set` with the moved body afterward.

```go
bodies, err := collide.NewBodies(9, 7)
if err != nil {
	return err
}
// Move in a fixed order, such as ascending ID, so every run gives the same result.
for i := range units {
	result, err := world.MoveAmong(bodies, units[i], dx[i], dy[i])
	if err != nil {
		return err
	}
	units[i] = result.Body
	if err := bodies.Set(units[i]); err != nil {
		return err
	}
	if result.X.Body != 0 {
		// Another body stopped this unit along X.
	}
}
```

- The mover ignores its own entry. A body that starts overlapping another body returns `ErrEmbedded`, so place bodies apart.
- Bodies that move earlier in the order win contested space. That is a deliberate bias, not a fairness rule; if your game needs another one, it is game code.
- `bodies.Overlaps` finds the bodies near a point, in ascending ID order, for targeting and area checks. `Remove` takes a body out.
- Aim with `Vec2.WithLength`, which scales a vector to a length with integer arithmetic and rounds each component to the nearest unit: a bolt can fly toward a target on a line that is not one of the compass directions, and still replay exactly.

Goje does not decide what happens when units jam. The [skirmish](../specs/skirmish-m13.md) shows one policy in game code: wait, ask for a new route around the other units, and give up after three tries.

## Build a world from a level

A version 2 [level](../specs/level-v2.md) can hold solid and trigger zones that you draw in the [Level Builder](levels.md#draw-zones). `collide.LevelShapes(board, wall)` returns one solid for each `#` cell, at the height you choose, and one solid or trigger for each zone. Add shapes of your own, then call `collide.New`, or call `collide.FromLevel(board, wall)` if the level is all you need.

```go
solids, triggers, err := collide.LevelShapes(board, collide.Interval{Bottom: 0, Top: 32})
if err != nil {
	return err
}
world, err := collide.New(width, height, solids, triggers)
```

A zone keeps its own ID, so read `board.Zones()` to find out which ID is the deck and which is the door, by `Kind`. Do not rely on the numbers: they change when an author adds or deletes zones. A wall cell has the ID `collide.WallID(width, x, y)`.

Rectangles are the only shape, and a `World` cannot change during a tick. The [measurements](collision-performance.md) show the cost of 2,048 moving bodies and of a 128 against 128 fight on both targets.

# M12 movement and collision

This is the accepted behavior for the [M12 movement and collision RFC](../rfcs/0013-m12-movement-and-collision.md). It covers the `engine/fixed` and `engine/collide` packages and the free-movement yard fixture in the playground. It does not change [M3 unit commands](unit-commands-m3.md), which keep their tile-to-tile routing.

## Scope and ownership

Goje owns exact arithmetic, shape overlap, movement against static solids, and trigger events. The game owns what a shape means, how fast a body moves, and what an event does. Routing, steering between bodies, damage, and physics are outside this contract.

## Contract

### Numbers and units

- `fixed.Num` is a signed 32-bit count of 1/256 units. Addition, subtraction, multiplication, division, negation, and absolute value saturate at the ends of the range instead of wrapping. Multiplication and division round to the nearest unit, with ties away from zero. Division by zero saturates toward the sign of the dividend, and zero divided by zero is zero. No operation reads a floating-point value, so results are identical on every target.
- A ground position is a `fixed.Vec2` in tiles on the same X and Y axes as `engine.WorldItem.GroundX` and `GroundY`. A footprint is a half-open `fixed.Rect`. An empty footprint overlaps nothing, and footprints that only touch along an edge or at a corner do not overlap.
- A vertical extent is a half-open `collide.Interval` of whole projected pixels, the unit of `engine.WorldItem.Elevation`. Two shapes overlap only if their footprints and their vertical extents both overlap. Collision never reads paint order, the camera, or the projection.

### Worlds

- `collide.New` builds an immutable world for a board of 1–128 tiles on each side from a list of solids and a list of triggers. Each shape must have a non-empty footprint and vertical extent and lie inside the board. IDs must be unique within each list; a solid and a trigger may share an ID. A world holds at most 65,536 solids and 65,536 triggers.
- A world is safe for concurrent use. Games replace the world to add or remove shapes.

### Moving

- `World.Move` moves a body along X by `dx` and then along Y by `dy`. For each axis it stops the body flush against the nearest solid whose vertical extent overlaps the body's, or flush against the board edge. The sweep covers the whole step, so a large step cannot skip a thin solid.
- A step that lands exactly flush is not blocked. A shortened step reports a `Contact` for its axis: the lowest solid ID among solids at the same distance, or the board edge. Being blocked on one axis does not cancel the other, so a body slides along a wall.
- `Move` returns `ErrOutside` for a body that is not fully inside the board and `ErrEmbedded` for a body that already overlaps a solid. It does not push the body out. It returns `ErrInvalid` for an empty body.

### Queries and triggers

- `World.Overlaps` appends the solids and then the triggers that overlap a rectangle and vertical extent, each group in ascending ID order. A query may extend past the board.
- A `Tracker` remembers which body and trigger pairs overlapped at its previous `Update`. `Update` appends enter and exit events ordered by trigger ID and then body ID. The first call reports only entries, a body omitted from a later call exits what it overlapped, and a repeated body ID is treated as one body. A tracker belongs to one scene and is replaced when the scene restarts.
- After construction, `Move`, `Overlaps`, and `Update` allocate nothing once the caller's output buffers and the tracker's internal buffers have grown to fit.

### Yard fixture

- `GOJE_PLAYGROUND=yard` or `?fixture=yard` runs the authored courtyard's level and art with a freely moving scout. Arrow keys or **WASD** move it relative to the screen, as in the pickup encounter. Diagonal movement uses 181/256 of the axis speed so it is not faster. **R** restarts. The camera is fixed, and mouse input has no effect.
- The level's walls and the lookout block the scout. A three-tile deck at height 24–32 does not block it, because its vertical extent starts above the scout's. A doorway trigger in the northern gap turns a lamp on while the scout overlaps it. The scene draws the scout's footprint over every other item.
- The yard is a separate fixture because the courtyard assigns the arrow keys and **W/S** to camera control.

## Errors and limits

Construction errors wrap `ErrInvalid`. `ErrOutside` and `ErrEmbedded` identify the offending body, and `ErrEmbedded` also names the lowest overlapped solid ID. The yard requires a board of at least four tiles on each side.

## Verification

- Unit tests compare `World.Overlaps` against a scan of every shape, `World.Move` against a one-unit-at-a-time reference, and `Tracker.Update` against a set-difference reference, on randomized worlds from fixed seeds. They also cover thin solids, ties, interior corners, vertical extents, and the board edge.
- A recorded 385-tick input history for the yard checks a hash of every position, the doorway events, the slide, and the walk beneath the deck. `make test-wasm` runs the same tests under WebAssembly, and the hash matches the native run.
- `make smoke-desktop` and `make smoke-web` drive the yard with scripted and real key input and check rendered pixels in OpenGL and Chromium WebGL.
- `make benchmark-collide` measures 2,048 bodies on both targets. See the [collision measurements](../docs/collision-performance.md).

## References

[RFC 0013](../rfcs/0013-m12-movement-and-collision.md), [`engine/fixed`](../engine/fixed/fixed.go), [`engine/collide`](../engine/collide/collide.go), and the [yard scene](../cmd/playground/yard.go).

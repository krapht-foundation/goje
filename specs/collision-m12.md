# M12 movement and collision

This is the accepted behavior for the [M12 movement and collision RFC](../rfcs/0013-m12-movement-and-collision.md), with the additions of the [M13 RFC](../rfcs/0014-m13-crowds-authored-collision-and-combat.md) marked below. It covers the `engine/fixed` and `engine/collide` packages and the free-movement yard fixture in the playground. It does not change [M3 unit commands](unit-commands-m3.md), which keep their tile-to-tile routing.

## Scope and ownership

Goje owns exact arithmetic, shape overlap, movement against static solids and against a set of moving bodies, and trigger events. The game owns what a shape means, how fast a body moves, and what an event does. Routing, steering, damage, and physics are outside this contract. M13 added bodies that block each other, but the rules for a crowd, such as who gives way, remain game code: see the [skirmish specification](skirmish-m13.md).

## Contract

### Numbers and units

- `fixed.Num` is a signed 32-bit count of 1/256 units. Addition, subtraction, multiplication, division, negation, and absolute value saturate at the ends of the range instead of wrapping. Multiplication and division round to the nearest unit, with ties away from zero. Division by zero saturates toward the sign of the dividend, and zero divided by zero is zero. No operation reads a floating-point value, so results are identical on every target.
- `fixed.Vec2.WithLength(n)` (M13) returns the vector in the same direction with length `n`, using integer arithmetic only. Each component is the exact value `x·n/√(x²+y²)` rounded to the nearest unit, with ties away from zero, and saturated at the ends of the range, for every vector. A zero vector stays zero and a negative `n` points the other way. Games use it to aim without a floating-point square root.
- A ground position is a `fixed.Vec2` in tiles on the same X and Y axes as `engine.WorldItem.GroundX` and `GroundY`. A footprint is a half-open `fixed.Rect`. An empty footprint overlaps nothing, and footprints that only touch along an edge or at a corner do not overlap.
- A vertical extent is a half-open `collide.Interval` of whole projected pixels, the unit of `engine.WorldItem.Elevation`. Two shapes overlap only if their footprints and their vertical extents both overlap. Collision never reads paint order, the camera, or the projection.

### Worlds

- `collide.New` builds an immutable world for a board of 1–128 tiles on each side from a list of solids and a list of triggers. Each shape must have a non-empty footprint and vertical extent and lie inside the board. IDs must be unique within each list; a solid and a trigger may share an ID. A world holds at most 65,536 solids and 65,536 triggers.
- A world is safe for concurrent use. Games replace the world to add or remove shapes.

### Moving

- `World.Move` moves a body along X by `dx` and then along Y by `dy`. For each axis it stops the body flush against the nearest solid whose vertical extent overlaps the body's, or flush against the board edge. The sweep covers the whole step, so a large step cannot skip a thin solid.
- A step that lands exactly flush is not blocked. A shortened step reports a `Contact` for its axis: the lowest solid ID among solids at the same distance, or the board edge. Being blocked on one axis does not cancel the other, so a body slides along a wall.
- `Move` returns `ErrOutside` for a body that is not fully inside the board and `ErrEmbedded` for a body that already overlaps a solid. It does not push the body out. It returns `ErrInvalid` for an empty body.

### Bodies and moving among them (M13)

- `collide.Bodies` is a mutable set of moving shapes on one board, each with a nonzero ID chosen by the game and unique within the set. `Set` inserts or replaces by ID, `Remove` deletes, `Get` reads, and `Overlaps` appends the IDs of bodies that overlap a rectangle and vertical extent, in ascending order. A set holds at most 4,096 bodies. It is not safe for concurrent use; the `World` it is used with stays immutable and safe for concurrent use. A nil set has no bodies.
- `World.MoveAmong(bodies, body, dx, dy)` behaves like `Move` and also stops the body flush against the nearest member of the set whose vertical extent overlaps its own, ignoring the entry that has the mover's own ID. It does not store the result: the caller calls `Set`. `Move` is `MoveAmong` with no set.
- `Contact.Body` is the nonzero ID of the body that stopped an axis. A solid or the board edge at the same distance is reported instead, with `Body` zero, and the lowest ID wins among bodies. A body that starts overlapping another body returns `ErrEmbedded`, and the error names both. Games must not spawn bodies on top of each other, and moving bodies in a fixed order, such as ascending ID, makes a crowd deterministic. Bodies earlier in the order win contested space.
- Each tile lists the bodies that touch it, reserving room for eight. After a tile has grown past that once, `Set`, `Remove`, `Overlaps`, and `MoveAmong` allocate nothing.

### Authored worlds (M13)

- `collide.LevelShapes(board, wall)` returns the solids and triggers of a decoded [level](level-v2.md): one solid per `#` cell with the vertical extent `wall`, which the game chooses because a level does not give wall heights, and one solid or trigger per zone with its own ID and vertical extent. `collide.FromLevel(board, wall)` builds a `World` from them. A game that wants shapes of its own, such as a solid for a placement, appends them to `LevelShapes` before `New`.
- A wall cell's ID is `collide.WallID(width, x, y)`, which is `1<<32` plus the cell's row-major index, so it never matches a zone ID. The game reads `Board.Zones()` to give each zone ID a meaning.

### Queries and triggers

- `World.Overlaps` appends the solids and then the triggers that overlap a rectangle and vertical extent, each group in ascending ID order. A query may extend past the board.
- A `Tracker` remembers which body and trigger pairs overlapped at its previous `Update`. `Update` appends enter and exit events ordered by trigger ID and then body ID. The first call reports only entries, a body omitted from a later call exits what it overlapped, and a repeated body ID is treated as one body. A tracker belongs to one scene and is replaced when the scene restarts.
- After construction, `Move`, `Overlaps`, and `Update` allocate nothing once the caller's output buffers and the tracker's internal buffers have grown to fit.

### Yard fixture

- `GOJE_PLAYGROUND=yard` or `?fixture=yard` runs [`levels/yard.json`](../cmd/playground/levels/yard.json), a version 2 level, with the courtyard's art and a freely moving scout. Arrow keys or **WASD** move it relative to the screen, as in the pickup encounter. Diagonal movement uses 181/256 of the axis speed so it is not faster. **R** restarts. The camera is fixed, and mouse input has no effect.
- The level's walls and the lookout block the scout. The level's solid zone of kind `deck`, three tiles at height 24–32, does not block it, because its vertical extent starts above the scout's. Its trigger zone of kind `door` in the northern gap turns a lamp on while the scout overlaps it. The Level Builder can edit both zones, and the fixture reads them by kind, not by ID. The scene draws the scout's footprint over every other item.
- The yard is a separate fixture because the courtyard assigns the arrow keys and **W/S** to camera control.

## Errors and limits

Construction errors wrap `ErrInvalid`. `ErrOutside` and `ErrEmbedded` identify the offending body, and `ErrEmbedded` also names the lowest overlapped solid ID. The yard requires the courtyard's scout and lookout placements, a trigger zone of kind `door`, and a solid zone of kind `deck`.

## Verification

- Unit tests compare `World.Overlaps` against a scan of every shape, `World.Move` against a one-unit-at-a-time reference, and `Tracker.Update` against a set-difference reference, on randomized worlds from fixed seeds. They also cover thin solids, ties, interior corners, vertical extents, and the board edge.
- A recorded 385-tick input history for the yard checks a hash of every position, the doorway events, the slide, and the walk beneath the deck. `make test-wasm` runs the same tests under WebAssembly, and the hash matches the native run.
- `make smoke-desktop` and `make smoke-web` drive the yard with scripted and real key input and check rendered pixels in OpenGL and Chromium WebGL.
- `make benchmark-collide` measures 2,048 bodies on both targets. See the [collision measurements](../docs/collision-performance.md).
- M13 tests compare `MoveAmong` against a one-unit-at-a-time reference, cover ties between solids and bodies, an embedded start, board edges, removal, and vertical extents between bodies, and compare `WithLength` with a floating-point reference, which only the test uses. The authored yard reproduces the same 385-tick position hash as the version that wrote its deck and door in Go. `make smoke-zone-roundtrip` authors the yard's zones in the Level Builder and plays them on desktop and web.

## References

[RFC 0013](../rfcs/0013-m12-movement-and-collision.md), [RFC 0014](../rfcs/0014-m13-crowds-authored-collision-and-combat.md), [`engine/fixed`](../engine/fixed/fixed.go), [`engine/collide`](../engine/collide/collide.go), and the [yard scene](../cmd/playground/yard.go).

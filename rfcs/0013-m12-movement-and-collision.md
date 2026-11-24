# RFC 0013: M12 - Movement and collision

**Status:** Implemented and verified for M12.

## Problem and goal

Goje can draw depth-sorted isometric sprites and route a unit over a tile grid, but it has no reusable way to ask where an actor may stand. The [M3 courtyard](../specs/unit-commands-m3.md) moves between tile centers, so a wall is a blocked cell and nothing else. [Beacon](../docs/playground.md#use-the-playground-in-your-game) hard-codes a lane and clamps the tomato to it. A game that wants a character to walk freely, slide along a wall, trigger a doorway, or pass under a bridge would have to write all of that itself.

A developer should be able to give actors a footprint, move them through a world of solid obstacles and trigger zones, and get the same result on desktop and web every time. The reference courtyard should demonstrate it with a character that moves freely, slides along walls, fires a trigger, and walks under a raised bridge without being blocked by it.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Delivery | Build and test the collision rules headlessly first, then add a spatial index and benchmark, then a playable scenario on both targets. | Start with the playable scenario. It shows progress sooner, but the rules would be shaped by one game and verified only through pixels. |
| Numbers | Integer fixed point: positions in 1/256 of a tile and vertical extents in whole projected pixels, with 64-bit intermediates. Results are exact and identical on every target. | `float64`. It matches the renderer and the `WorldItem` fields, but Go may fuse multiply-add differently across architectures, so results could differ between native arm64, amd64, and WebAssembly. The [runtime spec](../specs/runtime-m1.md#host-timing-and-input) already declines to promise cross-machine replay; the collision rules should not make that worse. |
| Math types | A small `engine/fixed` package that owns the fixed-point number, vector, and rectangle types and their saturating arithmetic. The collision package builds on it, and later simulation code (commands, AI, steering) can reuse it. | Define the types inside the collision package. It is less to build now, but the next system that needs exact arithmetic would either depend on collision or copy the types. |
| Shape | Axis-aligned rectangular footprints on the ground plane, plus a vertical interval. Static solids and triggers use the same shape. | Circles and capsules move more smoothly around corners but need square roots or floating point, and they complicate exact tests. Add them only when a game needs them. |
| Resolution | Move one axis at a time in a fixed order (X, then Y) and stop at the first contact on each axis, so a body slides along a wall. | Continuous swept-time resolution handles corners better and supports fast projectiles, but is more code and has more edge cases. A per-axis move tests the whole swept region, so it cannot tunnel through a solid at any speed. |
| Index | A uniform grid of buckets, one per tile, with deterministic ordering. | A quadtree or bounding-volume hierarchy. Neither is justified until a measured scene needs it. |
| Ownership | Goje owns geometry, movement, overlap queries, and trigger events. The game decides what a body or trigger means. | Put combat or navigation rules in the engine. These belong to later milestones and to each game. |

## Proposed design

### Coordinates and units

- The `engine/fixed` package defines `Num`, a signed 32-bit count of 1/256 units; `Vec2`, a pair of `Num`; and `Rect`, a half-open rectangle `[Min, Max)` of two `Vec2`. Arithmetic uses 64-bit intermediates, rounds multiplication and division to the nearest unit with ties away from zero, and saturates at the ends of the range rather than wrapping, so an overflow is deterministic and cannot flip a sign. `FromFloat` rejects non-finite and out-of-range values, and exists for authoring and tests; simulation never calls it.
- A **ground position** is a `fixed.Vec2` in tiles, the same X and Y axes as `engine.WorldItem.GroundX` and `GroundY`. One tile is 256 units, so a 128-tile board spans 32,768 units per axis and positions fit comfortably in 32 bits.
- A **vertical extent** is a half-open interval `[Bottom, Top)` of `int32` in whole projected pixels, the unit of `WorldItem.Elevation`. It is independent of paint order, so a draw-order change never alters collision.
- A **footprint** is a `fixed.Rect` on the ground plane. An empty rectangle or empty vertical interval never collides. Two shapes overlap only if they overlap on both ground axes and vertically; touching edges do not overlap.
- Conversions to `float64` for drawing happen once, at the presentation boundary, with `Num.Float64`. Simulation never reads a float. Floating-point vectors and matrices are not part of this milestone; the camera and projection keep their scalar form until a feature needs more.

### Bodies, solids, and triggers

- A **solid** is an immovable shape with a game-chosen `uint64` ID. A **trigger** is a shape that reports overlaps but never blocks. A **body** is a movable footprint with a vertical interval and an ID; it is blocked by solids and ignores other bodies in the first version. Body-to-body blocking and separation are left to a later decision because they need a tie-break policy and a crowd test.
- A game builds the static world once, at scene entry, from its own data (for example, one solid per blocked level cell). Solids and triggers are not added or removed during ticks in the first version; this keeps the index immutable and the tick allocation-free. A game that needs a moving platform or a door can replace the world on a transition.
- Each solid carries a vertical interval. A body is blocked only by solids whose interval overlaps its own, so a bridge deck from 24 to 32 pixels does not block a character from 0 to 20 walking below it.

### Moving and sliding

- `Move(body, dx, dy)` applies the X component, then the Y component. For each axis it finds the nearest solid that overlaps the region the body sweeps through on that axis, stops the body flush against it, and reports the contact. Flush means the footprints touch without overlapping, so the next move away is free. The sweep covers the whole step, so a large step cannot skip a thin solid; a longer step only costs more index lookups.
- The board edge is an implicit solid. A body stops flush against it, and the contact reports the edge instead of a solid ID. Tick speed is the caller's choice; the engine does not scale by time.
- The result reports the final position, the distance actually moved on each axis, and a contact for each axis whose move was shortened, with the lowest solid ID winning a tie. A step that lands exactly flush is not a contact. A body already overlapping a solid at the start of a move, which can happen if a game spawns it inside one, is not moved and returns a distinct error instead of being pushed out in a surprising direction. A body outside the board is rejected the same way.
- Order is deterministic: axis order is fixed, and ties among solids at the same distance use the lowest ID. Results do not depend on insertion order or map iteration.

### Triggers and overlap queries

- After a tick's moves, the game asks for trigger events. For each body the world compares the set of triggers it overlaps now with the set from the previous query and returns enter and exit events ordered by trigger ID, then body ID. A body's first query reports only entries. The game consumes events within the same tick; they are not queued across ticks.
- `Overlaps(rect, interval)` returns solids and triggers intersecting an arbitrary shape in ID order, for area checks such as melee reach or a build site. It appends to a caller-provided buffer.

### Index, storage, and allocation

- The world stores shapes in one slice and a bucket table keyed by tile. Queries visit only buckets the swept shape touches and sort or merge results by ID so order matches a brute-force scan.
- Construction allocates. `Move`, `Overlaps`, and event queries do not allocate in steady state when the caller reserves their output buffers.
- The first version caps the board at the existing 128-tile side and a bounded number of shapes, and validates both at construction with clear errors. Invalid shapes (negative extents, coordinates outside the board) are rejected at construction, not in the hot path.

### Presentation and the scenario

- Draw code converts positions to `WorldItem` fields as before. The engine does not draw collision shapes. The yard fixture draws the scout's footprint over every other item, so a viewer can see where the scout stops.
- The reference scenario is a separate fixture, `yard`, that reuses the authored courtyard's level, art, and camera. The courtyard assigns the arrow keys and W/S to camera pan and zoom in its accepted [M3 contract](../specs/unit-commands-m3.md), so free movement could not share it without redefining that contract. The yard walks the scout with the keyboard. The level's walls block and slide, a doorway trigger lights a lamp on enter and exit, and a raised deck lets the scout walk beneath it. A low wall that still blocks is covered by unit tests rather than the scene. A held key is applied every tick it is held, not once per frame, so catch-up ticks stay correct.
- Focus loss clears held movement, which the host already guarantees for every scene. Restart builds a new world, tracker, and scout at the spawn with the doorway unlit.

### Determinism and replay

- The collision package depends on nothing but integers, so one input history produces the same bodies, contacts, and trigger events on every target. The final slice records a short input history and checks that native and WebAssembly runs produce identical final positions and event sequences.
- This does not make the whole runtime replayable. The host loop's timing and the scene's other state remain outside this guarantee until a later replay milestone.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Fixed-point types and collision rules | The `engine/fixed` package has tests for known products and quotients, rounding of ties in both signs, saturation at both ends, `FromFloat` rejection of NaN, infinities, and out-of-range values, and rectangle emptiness, overlap, and touching edges. The headless collision package defines its types and `Move` and `Overlaps`. Tests cover flush contact, sliding along a wall, an interior corner, touching edges, stacked vertical intervals (walking under a bridge, being blocked by a low wall), a spawn inside a solid, an oversize step, and tie-breaks by ID. A brute-force reference scan is compared with the index on randomized small worlds from a fixed seed, and all of these tests pass under `GOOS=js GOARCH=wasm`. |
| 2. Triggers, index, and measurements | Enter and exit events match a reference on small layouts, including a body that spawns inside a trigger and one flush against the board edge. A benchmark places 2,048 bodies in a dense world with a representative number of solids. It reports allocations, move and query time, and percentiles on native and in Chromium, and shows zero steady-state allocations. No hardware-specific CI limit is set. |
| 3. Playable scenario | The yard scout moves freely with the keyboard, slides along a wall, triggers the doorway, and passes under the deck, on desktop OpenGL and Chromium WebGL. Real input drives each case and rendered pixels confirm it. A recorded input history yields identical positions and events on native and WebAssembly, focus loss clears held movement, and restart restores the scenario. Document the workflow and the units. |

## Results

All three slices are delivered. The [behavior specification](../specs/collision-m12.md) records the contract, the [guide](../docs/collision.md) shows how to use it, and the [measurements](../docs/collision-performance.md) report cost.

- The collision tests compare the index, `Move`, and the tracker with simple references on randomized worlds. They pass natively and under WebAssembly, and `make test-wasm` runs them in CI.
- Two deliberate changes to the draft came from testing. A swept region test cannot tunnel at any speed, so `Move` has no oversize-step error. A solid can never tie with the board edge, because a solid inside the board cannot begin at the edge, so the tie rule was dropped.
- A warm tick with 2,048 bodies allocated nothing. Natively it took a median of about 102 µs, and Chromium's rounded median was 300 µs, against a 16.7 ms step. Native, Node WebAssembly, and Chromium produced the same state hash.
- The yard's recorded 385-tick history produces the same position hash natively and as WebAssembly. The desktop smoke passed on macOS OpenGL, and the browser smoke passed in headless Chromium four times in a row.
- Not checked: Linux and other browsers, real keyboard auto-repeat on a physical keyboard, layouts with a few very large solids, and body-to-body blocking, which is out of scope.

## Out of scope

Rigid-body physics, mass, forces, joints, stacking, and restitution. Circles, capsules, polygons, and swept continuous resolution. Body-to-body blocking, crowd steering, and avoidance. Floating-point vectors, matrices, and quaternions, and any change to the camera or projection types. Pathfinding changes: the grid router keeps routing and does not learn about collision. Projectiles, damage, and combat rules. Moving or destroyable solids during a tick. Networked or recorded replay beyond the slice 3 check, and any change to the [level format](../specs/level-v1.md).

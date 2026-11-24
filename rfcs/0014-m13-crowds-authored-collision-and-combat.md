# RFC 0014: M13 - Crowds, authored collision, and combat

**Status:** Implemented and verified for M13. See [Implementation notes and results](#implementation-notes-and-results) for where the delivered milestone differs from this proposal.

## Problem and goal

[M12](0013-m12-movement-and-collision.md) gave Goje exact fixed-point movement against static solids and triggers, but three gaps keep it from supporting an RTS-like game.

1. Units do not block each other. [M3](../specs/unit-commands-m3.md) routes one scout tile to tile around a wall and a stationary lookout, and [M12](../specs/collision-m12.md) moves one scout freely. Neither lets several moving units share ground.
2. The yard's deck and doorway trigger are written in Go. A level author cannot place a solid or a trigger, so every collision layout needs a programmer.
3. Nothing can hurt anything. There are no projectiles, health, or opponents.

A developer should be able to author a level with solids and triggers in the Level Builder, command several units across it with the mouse, watch them block and go around each other, and fight guards that wake when the squad crosses a trigger. Desktop and web must agree exactly. The reference scenario is a new `skirmish` fixture: the player's scouts against guards, on a map whose raised deck and alarm trigger came from the Level Builder.

This milestone combines three ideas that were separate options: collision-aware crowd movement, level-authored collision, and combat basics. They share one scenario and one set of new engine primitives, so they are delivered as ordered slices and committed once.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Delivery | Engine primitives, then authored collision, then crowd movement, then combat, then the playable skirmish. Each slice has its own headless tests and exit check, and the milestone is committed and pushed once, at the end. | Ship the three ideas as separate milestones. Each would be smaller to review, but the combined scenario would need all of them anyway. |
| Body blocking | Bodies block each other. A mutable `collide.Bodies` set holds moving bodies, and `World.MoveAmong` sweeps a body against the static world and that set. A game moves bodies in ascending ID order, and each sees the others at their current positions. | Resolve all bodies together and push them apart. That is fairer, but it needs iteration, tie-breaking, and a notion of push direction that the exact one-axis sweep does not have. Moving in ID order favors low IDs, which is a deliberate, documented bias. |
| Where policy lives | Orders, route following, stuck handling, targeting, weapons, and health are reference-game code in `examples/skirmish`, which the playground and the benchmark share. The engine gains only what every game needs: bodies, `MoveAmong`, a world built from a level, and exact vector scaling. | Put crowd and combat in the engine now. Goje would fix rules before a second game shows which ones are general. Promote code from `examples/skirmish` when a second game needs it. |
| Zone format | A new optional `zones` array in a version 2 level: tile-aligned rectangles with a game-defined `kind`, a `solid` or `trigger` role, and a vertical extent in projected pixels. `#` cells stay as they are. | Arbitrary fixed-point rectangles. They are more expressive, but the Level Builder is a grid editor and the first scenario needs only tile-aligned shapes. |
| Level versioning | Version 2 adds `zones`. The decoder reads versions 1 and 2. The encoder writes the lowest version that can represent the document, so a level without zones is byte-identical to today's version 1 and every existing fixture, tool, and test keeps working. | Always write version 2. That is simpler to explain, but it rewrites every exported level and makes old tools reject files that use no new feature. |
| Controls | The skirmish keeps the M3 controls: left click selects, right click moves the selected scout, arrows pan, **W/S** zoom, **R** restarts. Scouts fire automatically at the nearest guard in range. | Add attack-move, focus fire, and hold fire. They are normal RTS commands, but each needs its own contract and is not needed to show blocking, projectiles, and damage. |
| Friendly fire | A bolt damages the first unit it touches, whichever team it belongs to. | Filter by team. That needs a per-body mask in the sweep, which is more surface for a first version. |
| Projectile aiming | A bolt moves along the exact direction to its target. `fixed.Vec2.WithLength` scales a vector to a given length with an integer square root, so aiming is deterministic. | Restrict bolts to the eight compass directions. That avoids a square root, but shots would visibly miss targets that are not on a line. |

## Proposed design

### New engine primitives

- **Bodies.** `collide.Bodies` is a mutable set of moving shapes on one board, with a nonzero game-chosen ID for each. It supports `Set` (insert or replace by ID), `Remove`, and `Overlaps`, which appends the IDs of bodies overlapping a rectangle and vertical extent in ascending order. It is not safe for concurrent use. `World` stays immutable and safe for concurrent use.
- **Moving among bodies.** `World.MoveAmong(bodies, body, dx, dy)` behaves like `Move` and also stops flush against the nearest body whose vertical extent overlaps the mover's, ignoring any entry with the mover's own ID. It does not store the new position; the caller does that with `Set`. `Contact` gains a way to report that a body stopped the move and which one. A solid wins a tie with a body, and the lowest ID wins among bodies. A body that starts overlapping another body returns `ErrEmbedded`, as for a solid; games must not spawn bodies on top of each other.
- **Level worlds.** `collide.FromLevel(board, wall)` builds a `World` from a decoded level: one solid per `#` cell with the vertical extent the game chooses, and one solid or trigger per zone. A zone keeps its own ID; a wall cell gets an ID above the 32-bit range. The game reads `board.Zones()` to give each ID a meaning.
- **Exact direction.** `fixed.Vec2.WithLength(n)` returns the vector in the same direction with length `n`, rounded to the nearest unit, using integer arithmetic only. A zero vector stays zero.
- After the caller's buffers and the set's buckets have grown, `MoveAmong`, `Set`, `Remove`, and `Overlaps` allocate nothing.

### Authored collision

- **Level version 2.** The document gains a required `zones` array of 0–1,024 objects, and its `version` is `2`. A zone requires `id`, `kind`, `role`, `x`, `y`, `w`, `h`, `bottom`, and `top`. The ID is a unique integer among zones from 1 to 4,294,967,295. `kind` has the same shape as a placement kind and is chosen by the game. `role` is `solid` or `trigger`. The rectangle `[x, x+w) × [y, y+h)` in cells must lie inside the board with `w` and `h` at least 1. The vertical extent `[bottom, top)` uses whole projected pixels with `0 <= bottom < top <= 1024`. Zones may overlap each other, walls, and placements. The existing rejection of missing, duplicate, unknown, and null fields, fractional numbers, and trailing data applies. The 256 KiB limit is unchanged. A new specification describes version 2, and the version 1 specification stays as accepted.
- **Reader and writer.** `level.Decode` accepts both versions, and a version 1 board has no zones. `level.Encode` takes zones and writes version 1 when there are none and version 2 otherwise. A version 1 document therefore round-trips byte for byte, and `levelcheck` and `projectcheck` accept version 2 files through the same decoder.
- **Level Builder.** The editor opens both versions and preserves zones it does not edit. A new zone tool adds a zone by dragging a rectangle or by typing its cell coordinates, edits a zone's kind, role, rectangle, and vertical extent, and deletes it. Every zone edit is one undoable transaction under the existing history limits, and invalid edits are rejected with visible text. Zones appear as labeled overlays, and solids and triggers differ by pattern and label as well as color. Placements remain move-only as in [M4](0005-m4-level-builder.md), so the skirmish's units are listed in its level file by hand and validated by the same decoder.
- **The yard becomes authored.** The yard fixture builds its world from a version 2 level whose deck and doorway are zones, through `FromLevel`. Its recorded 385-tick history must still produce the position hash `13227287839575903946`, which proves that the authored world matches the one that was written in Go.

### Crowd movement

- Units are bodies in a `Bodies` set. The player's scouts and the guards use the same movement, so a guard blocks a scout like another scout does.
- A move command keeps the M3 contract for selection, picking, and goals: the router finds the first shortest cardinal route over walls and over cells held by units without orders, and an out-of-bounds, blocked, unreachable, or occupied goal leaves the current order unchanged. Cells held by moving units do not block routing, because those units will have moved on.
- A unit follows its route by moving along one axis toward the next cell center at a fixed speed, clamping the step so it lands exactly on the center. This needs no square root, and it keeps units on cell-center lines, so a unit passes another freely unless they share a line.
- A unit that is blocked by a body counts consecutive blocked ticks. After 30 it asks for a new route from the last center it reached, treating every cell held by another unit as blocked. If a route exists it follows that route. If none exists, or after three such repaths in one order, it gives up and becomes idle. Every order therefore ends. A request is only honored on ticks selected by the unit's ID, so a crowd that jams does not repath all at once.
- A unit commanded while between centers finishes its current edge first, as in M3. Two units meeting head-on in open ground each go around the other. Two units meeting head-on in a one-tile corridor both give up and stay, because there is no room to pass and units do not back out or swap places. This limit is documented, not hidden.

### Combat

- **Weapons.** A unit has health and a weapon with a range, a damage amount, and a cooldown in ticks. Each tick it picks the nearest enemy within range, with the lowest ID winning a tie, using `Bodies.Overlaps` and exact distances. If the weapon is ready, it fires one bolt along the exact direction to that enemy, then waits for the cooldown. A unit does not need to stop to fire.
- **Bolts.** A bolt is a small body with a velocity and a lifetime. It is not in the unit set, so it never blocks anything. Each tick it moves with `MoveAmong` against the static world and the units. If any axis is shortened, it is spent: it damages the unit it touched, if any, and disappears. Its vertical extent lies inside a unit's, below a deck, so it flies under a deck that stops nothing at ground level, and walls stop it.
- **Health.** Damage subtracts from health. A unit at zero health is removed from the set at the end of the tick, so removal order never changes what another unit did earlier in the same tick. The game shows health as a bar above each unit.
- **Guards.** A guard is in one of three states. It idles at its post until a scout comes within its sight radius or a scout enters an alarm trigger zone. It then chases the nearest scout by routing toward a cell in range, fires when it has one in range, and returns to idle when no scout is within the radius. Targets and routes use the same deterministic rules as the crowd. Guards do not check for a clear line of fire, so some shots hit walls. This is the simple AI, not tactics.
- **Tick order.** One tick runs in this order: input and commands, guard decisions, movement in ascending ID order, trigger events, weapon firing in ascending ID order, bolt movement in creation order, then removal of dead units. A specification records the order, and tests depend on it.
- **Determinism.** All of this uses `fixed` arithmetic. One input history produces identical units, bolts, health, and events on every target.

### The skirmish scenario

- `GOJE_PLAYGROUND=skirmish` or `?fixture=skirmish` runs a new fixture on a level authored with the Level Builder. It has walls, a raised deck over a lane that bolts can pass under, a one-tile doorway where a jam can occur, and an alarm trigger that wakes the guards. The level lists several scouts and guards by hand, because the editor does not add placements.
- The fixture reuses the courtyard's art and scout sprite clips and the M3 controls. It draws health bars with canvas rectangles and each bolt as a small rectangle. It never changes the courtyard or the yard.
- A defeated or victorious side is visible as the removal of every unit of the other side. **R** restarts and rebuilds all state from the level.

### Allocation and budget

- Unit, bolt, and route storage are sized at scene entry for at most 256 units and 512 bolts. Steady-state ticks allocate nothing. A benchmark runs a crowd of 128 scouts against 128 guards with active bolts on a 64 × 64 board and reports allocations, median and 99th percentile tick time, and the worst tick, including a tick that triggers many repaths. The recorded hash must match on native, Node Wasm, and Chromium. No CI time limit is set, but a worst tick above half of a 60 Hz frame on the development machine is a reason to revisit the repath spacing before shipping.

## Implementation and acceptance plan

| Slice | Exit check |
| --- | --- |
| 1. Engine primitives | `Bodies`, `MoveAmong`, the body contact, `FromLevel`, and `Vec2.WithLength` have unit tests, and the new collision paths are compared with a one-unit-at-a-time reference on randomized worlds from fixed seeds. Tests cover ties between solids and bodies, a mover ignoring its own entry, vertical extents between bodies, an embedded start, board edges, removal, and `WithLength` against a brute-force search for exact rounding. Everything passes natively and as WebAssembly, and the steady-state paths allocate nothing. |
| 2. Authored collision | The level package decodes and encodes version 2 with tests for every rejection above, a version 1 file that round-trips byte for byte, a version 2 file that decodes and encodes to the same bytes, and a short fuzz run. The editor core and browser tool add, edit, delete, undo, and redo zones. A Chromium round trip authors the yard's deck and doorway on the courtyard terrain, exports, reopens, runs `levelcheck`, embeds the file in a scratch copy of the playground, and shows the lamp lighting and the scout walking under the deck on desktop and web. The repository's authored yard reproduces the recorded position hash. |
| 3. Crowd movement | Headless tests cover command and route rules with occupied and unoccupied cells, an order replaced between centers, following a route with exact landings, two units going around each other in open ground, a corridor jam where both give up, a goal held by an idle unit, and restart. A randomized history from a fixed seed checks that no two footprints overlap on any tick and that every order ends within a bound. A recorded history hash matches on native and WebAssembly. |
| 4. Combat | Headless tests cover a bolt stopped by a wall, a bolt passing under a deck, damage and death, the cooldown, target ties, friendly fire, a guard waking on a trigger entry, chasing, returning to idle, and removal order within a tick. A recorded history of a full fight yields the same hash natively and as WebAssembly. The benchmark above runs on native, Node Wasm, and Chromium. |
| 5. Playable skirmish | Native OpenGL and Chromium WebGL smokes use real input: select a scout, right-click it through the doorway, see two scouts block and go around each other, see a bolt hit, see a health bar drop, see a guard disappear, and restart to the authored layout. Existing smokes for the courtyard, yard, and Beacon still pass. The docs describe the scenario, controls, level version 2, the editor zone tool, and measurements, and state the limits. |

## Out of scope

Group selection, box select, formations, and group commands. Pushing units apart, swapping places, backing out of corridors, steering and flocking, and diagonal movement. Attack-move, focus fire, hold fire, melee, area damage, armor, and weapon types. Line-of-sight checks for AI, tactics, fog of war, production, resources, and pathfinding that considers danger. Team filtering in the sweep. Sub-tile zones, zone shapes other than rectangles, zones that move or change during play, and adding or deleting placements in the Level Builder. Combat audio. Any change to the grid router, the version 1 level specification, the M3 courtyard contract, or the M12 yard's controls.

## Contracts this proposal changes

[RFC 0013](0013-m12-movement-and-collision.md) lists body-to-body blocking and crowd steering as out of scope, and [`specs/collision-m12.md`](../specs/collision-m12.md) says that steering between bodies and damage are outside its contract. This milestone lifts those limits only for the new `Bodies` API and the skirmish fixture. If the RFC is accepted, the collision specification gains the new API, and new level version 2 and skirmish specifications record the rest. Each change is stated in the document it affects, not applied silently.

## Implementation notes and results

The accepted behavior is recorded in the [level version 2](../specs/level-v2.md), [collision](../specs/collision-m12.md), and [skirmish](../specs/skirmish-m13.md) specifications. Where this proposal and those documents differ, the specifications are the contract. The delivered milestone differs from the proposal in these ways.

- **Package location.** The simulation is in `examples/skirmish`, not `internal/skirmish`. The reference playground is built from a separate module by `make check-example`, and a Go module cannot import another module's `internal` packages. The package is still reference game code, not an engine contract. Only the benchmark, which lives inside the repository, stays internal.
- **`WithLength` accuracy.** The contract is that each component is within one unit of the exact value, which the test checks against a floating-point reference on random vectors. The proposal's brute-force search for exact rounding was not built, and the integer algorithm does not promise nearest-unit rounding.
- **Allocation.** `Bodies` reserves room for eight bodies in every tile and room for removing 4,096 bodies when it is created, so play does not allocate the first time a unit visits a tile. The proposal only promised no allocation after buckets had grown. The trigger tracker still grows to its busiest tick, so the benchmark measures after 300 warm-up ticks.
- **Level Builder.** Zones are added by dragging a rectangle, clicking or pressing a key on two corners, or typing a rectangle. `levelcheck` stays specific to the courtyard's placements and now reports the zone count. It rejects the skirmish level, which has several scouts and guards by design.
- **Scenario.** The authored skirmish is a 10 × 8 board with a two-tile wall, a deck, and an alarm line, two scouts, and two guards. It has no one-tile doorway, so the headless tests, not the playable scenario, cover two units meeting in a corridor. The recorded desktop and Chromium smokes select a scout, order it across the alarm line, and watch the alarm ring, a bolt fly, health bars drop, units disappear, the selection clear, and **R** restore the opening. They do not show two scouts blocking and going around each other. That behavior is covered by headless tests, randomized histories, and the 128 against 128 benchmark, which jams and repaths, and it was not checked by eye in a window.
- **Guards.** A hunting guard routes toward the scout's own cell and stops walking while a scout is in range, instead of choosing a cell in range. A guard that stands down stops where it is; it does not walk back to a post. An alarm wakes every guard for good, and the guard hunts at any distance until no scout is left.
- **Smokes and CI.** New scripts are `scripts/smoke-zone-editor.mjs` and `make smoke-zone-roundtrip`, which runs the new `--yard` modes of the desktop and Chromium yard smokes. `scripts/benchmark-collide.mjs` takes the name of the program it runs. The native smoke now runs five fixtures in about twelve seconds, so the full `make smoke-desktop` allows 60 seconds instead of 20. CI runs `make smoke-zone-roundtrip`.

### Results

| Check | Result |
| --- | --- |
| Authored yard | The recorded 385-tick history reproduces position hash `13227287839575903946` from `levels/yard.json`, the same value the version written in Go produced. |
| Engine and level tests | Unit, reference-comparison, and fuzz tests pass natively, under the race detector, and as WebAssembly. A 20-second fuzz of the version 2 decoder found no failure. |
| Skirmish tests | Crowd, command, combat, AI, alarm, removal, and randomized no-overlap histories pass. The pinned crowd, squad, playground-fight, and benchmark traces are identical natively on macOS arm64, natively on Linux arm64 in a container for the packages that build without graphics, and under Node WebAssembly. |
| Benchmark | A 128 against 128 battle on a 64 × 64 board ran 600 measured ticks per run with zero allocations and the same trace on native, Node WebAssembly, and Chromium. Medians were 1.19 ms native and 3.6 ms in Chromium, and the worst tick in any run was 4.4 ms, under half of a 60 Hz frame. See the [measurements](../docs/collision-performance.md#measure-a-crowd-and-a-fight). |
| Smokes | `make smoke-desktop`, `smoke-web`, `smoke-level-editor`, `smoke-zone-roundtrip`, `smoke-level-roundtrip`, and `smoke-sprite-roundtrip` pass on macOS, including the skirmish with real input in OpenGL and Chromium WebGL and a Level Builder export played on both targets. |

### Not checked

Linux native smokes and other browsers beyond what CI runs, any GPU other than the development machine's, and the Level Builder in Firefox and Safari. Two scouts blocking each other in the playable scene, as described above. A worst-case board for repath cost, such as a long maze with many units asking for routes at once. Drawing cost of a full 256-unit scene. Level Builder performance with 1,024 zones.

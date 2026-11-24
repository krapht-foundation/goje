# M13 skirmish: crowds and combat

This is the accepted behavior for the [M13 RFC](../rfcs/0014-m13-crowds-authored-collision-and-combat.md). It covers the reference game simulation in [`examples/skirmish`](../examples/skirmish/sim.go) and the `skirmish` playground fixture. It builds on the [collision contract](collision-m12.md) and the [version 2 level format](level-v2.md), and keeps the [M3 controls](unit-commands-m3.md). The simulation is game code, not an engine contract: a second game may choose other rules, and code moves into the engine only when two games need the same rule.

## Scope and ownership

Goje supplies exact arithmetic, bodies and sweeps, level shapes, and routing. The skirmish package owns what a unit is, what an order means, how a crowd resolves a jam, who shoots whom, and in what order the rules run. It draws nothing and reads no clock, so one input history gives one result on every target. A `Sim` is for one goroutine.

## Level

`skirmish.New` takes a decoded level. Placements of kind `scout` and `guard` become units, listed in ascending ID order whatever their order in the file. Any other kind, a board of no cells, more than 256 units, or a unit that starts inside a solid is an error. Walls and solid zones are collision shapes 32 projected pixels tall, so a solid zone blocks a unit and a route only if its vertical extent overlaps the unit's 0–20. A raised deck, such as 24–32, blocks neither. A trigger zone of kind `alarm` is the alarm line. Other zones have no meaning to the simulation.

## Units and crowds

- A unit is a body with a 3/8-tile square footprint, a height of 20, and a team, `Scouts` or `Guards`. Units block each other through `collide.Bodies` and `World.MoveAmong`.
- Orders are cell routes. `Command(id, goal)` accepts an order only for a living unit and a goal that is on the board, not inside a wall or a solid zone that reaches a unit, and not held by a unit that has no order. It finds the first shortest cardinal route over cells that are free of walls and of idle units. Cells held by units that are walking do not block it, because those units will have moved on. An unreachable goal, or a stationary unit commanded to its own cell, leaves the current order alone. A unit between cell centers finishes its current edge first.
- A unit walks along one axis at 16/256 tile per tick, and the last step of each edge lands exactly on the next cell center, so units stay on cell-center lines and need no square root. A unit that arrives, or gives up, becomes idle and then blocks routes like any idle unit.
- A unit that could not move for 30 consecutive ticks asks for a new route, on a tick chosen by its ID so that a jam does not search all at once. The route starts from the last cell center it reached and treats every cell held by another unit as blocked. If no route exists, or the order has already asked for three new routes, the unit gives up and becomes idle. Every order therefore ends.
- Units move in ascending ID order, each seeing the others where they are, so a lower ID wins when two units contend for the same space. Two units meeting head-on in open ground each go around the other. Two units meeting head-on in a one-tile corridor both give up and stay, because units neither swap places nor back out.

## Combat

- Every unit carries one weapon with a range of 4 tiles between centers, 10 damage, and a cooldown of 30 ticks. Scouts have 40 health and guards have 30. Each tick a unit with a ready weapon fires one bolt at the nearest living enemy within range, with the lowest ID winning a tie. It does not stop to fire, and it checks only distance, not whether anything is in the way.
- A bolt is a 1/8-tile body flying at heights 8–12 at 1/4 tile per tick along the exact direction to its target, scaled with `fixed.Vec2.WithLength`. It starts 3/8 tile from the shooter's center, so it never overlaps the shooter. A bolt is not in the unit set, so it never blocks anything and units walk through it. Each tick it moves with `MoveAmong`. If it is stopped by anything, or fades after 24 ticks, it is spent. A unit it stopped at loses 10 health. A bolt that starts on a wall or a unit is spent at once, and one that would start off the board is dropped. A bolt that a unit walks into is resolved the same way on its next move. Bolts hurt every unit, so shots through a friend hit the friend. The bolt's height lets it pass under a raised deck and stops it at walls.
- A unit with no health left is removed at the end of the tick, after every other rule, so the order of the loops never decides who got to act. It stops blocking, shooting, and being targeted from the next tick. Units that die in the same tick all fire in it.
- At most 512 bolts fly at once. A unit that would fire while the pool is full keeps its ready weapon and fires on a later tick.

## Guards

A guard idles until a scout comes within 6 tiles, or until a scout enters an alarm zone, which wakes every guard once for the rest of the scene. A guard that has seen a scout hunts the nearest scout within 12 tiles, and an alarmed guard hunts the nearest scout at any distance until no scout is left. A hunting guard routes toward the scout's cell every 15 ticks, offset by its ID, and stops walking while a scout is in range. When no scout is within its radius it stands down and is idle again. Hunting guards ignore other units when they plan, so a jam is resolved by the same repath and give-up rule as for scouts. The AI does not check for a clear line of fire and does not use cover.

## Tick order

`Step` runs these in order: guard decisions, movement in ascending ID order, trigger events for the scouts, weapon firing in ascending ID order, bolt movement in the order the bolts were fired, removal of dead units, and the trace. Commands are applied between ticks by calling `Command`. A bolt fired in a tick also moves in that tick.

## Trace and limits

`Sim.Trace` is a 64-bit hash of every unit's position, health, goal, cooldown, and flags, and of every bolt, folded in after each tick. It depends on the whole history. Native, Node WebAssembly, and Chromium runs of the same history must produce the same value.

At most 256 units and 512 bolts. Route storage is sized when the scene is created, so `Step` allocates nothing once collision buckets and the trigger tracker have grown to fit their busiest tick. The simulation has no diagonal movement, no swapping, no formations, and no group commands.

## Fixture

`GOJE_PLAYGROUND=skirmish` or `?fixture=skirmish` runs [`levels/skirmish.json`](../cmd/playground/levels/skirmish.json) on the courtyard's art. Left click selects a scout, and right click orders it. Arrow keys pan, **W/S** zoom, and **R** restarts. Scouts are blue, guards are red, bolts are pale yellow, and each unit has a health bar that shows green for health and dark red for what is gone. A raised deck stands over the ground, a purple line of cells across the board is the alarm line and turns magenta when the alarm rings, and a defeated unit disappears from the board. The selection clears when its scout falls.

In the recorded history, scout 1 is selected and sent to cell `(6, 2)`. It crosses the alarm line, both guards wake, scout 1 and both guards fall, and scout 2 survives with 20 health.

## Verification

- Headless tests cover command rules, route following, a refused command that keeps the old order, routes around idle units, head-on units in open ground and in a corridor, orders that always end, solid zones by height, randomized histories from fixed seeds with no overlapping footprints and an identical trace on replay, zero allocations per tick, the weapon and cooldown, wall and deck behavior, friendly fire, target ties, guard waking by sight and alarm, standing down, and removal.
- A recorded fight in the playground scene and a recorded 128 against 128 battle have pinned traces that `make test-wasm` reproduces under WebAssembly.
- `make smoke-desktop` and `make smoke-web` select, order, and watch the recorded fight with real input and check rendered pixels in OpenGL and Chromium WebGL, then restart.
- `make benchmark-skirmish` measures 128 scouts against 128 guards on a 64 × 64 board on native, Node WebAssembly, and Chromium. See the [measurements](../docs/collision-performance.md#measure-a-crowd-and-a-fight).

## References

[RFC 0014](../rfcs/0014-m13-crowds-authored-collision-and-combat.md), [`examples/skirmish`](../examples/skirmish/sim.go), the [skirmish scene](../cmd/playground/skirmish.go), and the [benchmark](../internal/skirmishbench/skirmishbench.go).

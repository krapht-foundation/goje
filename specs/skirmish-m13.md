# M13 skirmish: crowds and combat

This is the accepted behavior for the [M13 RFC](../rfcs/0014-m13-crowds-authored-collision-and-combat.md), with the orders, group assignment, and snapshot of the [M14 RFC](../rfcs/0015-m14-text-rts-commands-and-replays.md) marked (M14) below, and the vision option and events of the [M15 RFC](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) marked (M15). It covers the reference game simulation in [`examples/skirmish`](../examples/skirmish/sim.go) and the `skirmish` playground fixture. It builds on the [collision contract](collision-m12.md) and the [version 2 level format](level-v2.md), and keeps the [M3 controls](unit-commands-m3.md). The simulation is game code, not an engine contract: a second game may choose other rules, and code moves into the engine only when two games need the same rule.

The M14 additions change no M13 behavior. A history that uses only `Command` and `Step` runs exactly as before, and its trace is the same value. The M15 additions change nothing while vision is off, which is the default: every M13 and M14 history keeps its trace and its snapshot bytes.

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

A guard idles until a scout comes within 6 tiles, or until a scout enters an alarm zone, which wakes every guard once for the rest of the scene. A guard that has seen a scout hunts the nearest scout within 12 tiles, and an alarmed guard hunts the nearest scout at any distance until no scout is left. A hunting guard routes toward the scout's cell every 15 ticks, offset by its ID, and stops walking while a scout is in range. When no scout is within its radius it stands down and is idle again. Hunting guards ignore other units when they plan, so a jam is resolved by the same repath and give-up rule as for scouts. The AI does not check for a clear line of fire and does not use cover. A guard given a `Move` order, as by `Command`, keeps running the AI as in M13. A guard carrying an `Attack` or `AttackMove` order (M14) skips the AI and returns to it when the order ends.

## Orders and stances (M14)

- `Sim.Order(id, Order{Kind, Goal, Target})` gives any living unit an order and reports whether it was accepted. The scene decides which units the player may command. A refused order leaves the current order alone, and an accepted one replaces it.
- `Move` is `Command`, with the same rules, and `Command` is `Order` with a `Move`.
- `Attack` names an enemy. It is refused for an unknown or dead target, a target on the unit's own team, and a target out of weapon range that no route reaches. Routes toward a target treat idle units other than the target as walls, as `Command` does. Each tick, before anyone moves, an attacking unit whose target has been removed stands idle, and one within weapon range of its target, center to center, stops walking. One out of range routes again toward the target's current cell when it has no route, and otherwise every 15 ticks, offset by its ID, keeping its count of blocked ticks and new routes, so the repath and give-up rule still ends a hopeless chase. A unit that has no route and finds none stands idle. An attacking unit shoots only at its target, and only while the target is within range; it ignores every other enemy.
- `AttackMove` walks to its goal under the `Command` rules. When the unit's turn to move comes and a living enemy is within weapon range, it does not move that tick and its blocked count resets; it shoots as any unit does. It resumes when no enemy is in range and ends at its goal. Range ignores walls, so an attack-move waits beside an enemy it cannot hit through a wall for as long as the enemy stays in range.
- `Sim.SetHold(id, hold)` sets the hold-fire stance of a living unit. A unit that holds its fire never shoots, though its weapon still cools down, and its attack-move does not stop for enemies. Orders leave the stance alone. `Unit.Hold` reports it.
- A `Move` or `AttackMove` order ends on arrival. Any order ends when its unit gives up under the repath rule, and an attack ends when its target is removed. A unit whose order ended stands idle. A removed unit loses its order.

## Selection and group orders (M14)

- `Sim.AssignGoals(dst, ids, click)` turns a group order into one goal per unit. A breadth-first search from the clicked cell over cells that are on the board, not solid for a unit, and not held by an idle unit lists cells nearer in steps first and row-major among cells at the same distance. The living units in `ids`, each once, sorted by the squared distance from their center to the center of the clicked cell and then by ID, take those cells in that order. Units beyond the number of reachable cells get no goal, and a click off the board, on a solid cell, or on an idle unit gives none. The search does not check that each unit can reach its cell, so an order to the cell may still be refused.
- `Sim.OrderGroup(ids, order)` gives a `Move` or `AttackMove` order to each unit with an assignment, in assignment order, each to its own goal, and an `Attack` order to every unit in `ids` unchanged. It returns how many units accepted. A unit without an assignment or whose order is refused keeps its current order. The group arrives spread out; it does not keep a shape while moving.
- With `dst` sized for `MaxUnits` entries, selection, assignment, and group orders allocate nothing; their search buffers are sized by `New`.

## Tick order

`Step` runs these in order: guard decisions, attack orders in ascending ID order (M14), movement in ascending ID order, trigger events for the scouts, weapon firing in ascending ID order, bolt movement in the order the bolts were fired, removal of dead units, the sight update when vision is on (M15), and the trace. Commands, orders, and stances are applied between ticks by calling `Command`, `Order`, `OrderGroup`, and `SetHold`. An attack-move's check for enemies in range happens at its turn in movement. A bolt fired in a tick also moves in that tick.

## Vision and events (M15)

- `Sim.EnableVision(VisionConfig{Radius})` turns on line of sight with a radius of 1 to 181 cells; it must be called before the first `Step`, at most once. With it on, the rules of the [vision specification](vision-m15.md#skirmish-rules) apply: only walls block sight, a unit fires at and an attack-move stops for an enemy only in its team's sight, an `Attack` needs its target in sight and ends at the cell where it lost the target, and a guard that is not alarmed wakes when its team sees a scout within 12 tiles instead of by the six-tile test. With it off, `Sim.Visible` and `Sim.Explored` report every cell on the board.
- `Sim.Events` returns what happened during the last `Step`: bolts fired, bolts that hurt a unit, units that fell, and the alarm. The events are output only. The [vision specification](vision-m15.md#events) defines them.

## Trace and limits

`Sim.Trace` is a 64-bit hash of every unit's position, health, goal, cooldown, and flags, and of every bolt, folded in after each tick. A unit's order kind, target, and stance are folded in only while one of them differs from an idle `Move` without hold (M14), so M13 traces are unchanged. With vision on, the explored cells and the last seen cells of attacking units are folded in after the bolts (M15). It depends on the whole history. Native, Node WebAssembly, and Chromium runs of the same history must produce the same value.

At most 256 units and 512 bolts. Route storage is sized when the scene is created, so `Step` allocates nothing once collision buckets and the trigger tracker have grown to fit their busiest tick. The simulation has no diagonal movement, no swapping, no formations, no queued orders, and no line-of-fire checks.

## Snapshot (M14)

`Sim.Snapshot` writes the whole dynamic state between ticks, and `Sim.Restore` reads it into a Sim built by `New` from the same level. Walls, zones, unit IDs, teams, and maximum health come from the level. Restoring and running on reaches the same states and trace as the uninterrupted run. The [save envelope](save-v1.md) adds the magic number, the game, the content check, and the checksum; the snapshot is the payload. All values are little-endian, and positions are raw 1/256-tile counts of `fixed.Num`. Version 1 is laid out as follows. A Sim with vision on writes and reads [version 2](vision-m15.md#snapshot-version-2) instead (M15), and each refuses the other's version.

| Part | Fields |
| --- | --- |
| Header, 33 bytes | `u16` version 1, `u16` board width, `u16` board height, `u16` unit count, `u64` tick, `u64` trace, `u64` bolt counter, `u8` alarm (0 or 1). |
| Each unit, in ascending ID order | `u64` ID, `i32` footprint minimum X and Y, `i32` health, `u8` flags (1 alive, 2 hunting, 4 alarmed, 8 hold fire), `u8` order (0 move, 1 attack, 2 attack-move), `u64` target (nonzero only for an attack), `u16` last cell center reached, `u16` cell center walked toward, `i32` goal cell or −1, `u16` ticks blocked, `u8` new routes in this order, `u8` weapon cooldown, `u16` index of the next route cell, `u16` route length, then that many `u16` route cells. Cells are row-major indices. |
| Bolts | `u16` count, then for each bolt in firing order `u64` ID, `i32` footprint minimum X and Y, `i32` velocity X and Y, `u8` ticks of life left. |
| Trigger state | `u32` count, then for each pair `u64` trigger ID and `u64` body ID, as `collide.Tracker.AppendOverlaps` lists them. |

`Restore` refuses, and leaves the Sim unchanged, for another version, board size, or unit count; units whose IDs differ from the level's; truncated or trailing data; unknown flags or order kinds; an attack without a target, or a target that is not an enemy; a cell, goal, or route cell off the board, or a next index past the route; a blocked count over 60, more than three new routes, or a cooldown over 30; a living unit whose health is not 1 to its maximum, or a dead one with health left; a living unit outside the board, inside a solid that reaches it, or overlapping another living unit; more than 512 bolts; a bolt whose ID is not newer than the last one, newer than the bolt counter, or not a bolt ID, whose life is not 1 to 24, whose speed exceeds 1/4 tile on an axis, or which is outside the board; and trigger pairs that are out of order, name an unknown trigger, or name a body that is not a scout. Every count is bounded by the level or by the remaining data before anything is allocated from it.

## Fixture

`GOJE_PLAYGROUND=skirmish` or `?fixture=skirmish` runs [`levels/skirmish.json`](../cmd/playground/levels/skirmish.json) on the courtyard's art. Left click selects a scout, and right click orders it. Arrow keys pan, **W/S** zoom, and **R** restarts. Scouts are blue, guards are red, bolts are pale yellow, and each unit has a health bar that shows green for health and dark red for what is gone. A raised deck stands over the ground, a purple line of cells across the board is the alarm line and turns magenta when the alarm rings, and a defeated unit disappears from the board. The selection clears when its scout falls.

In the recorded history, scout 1 is selected and sent to cell `(6, 2)`. It crosses the alarm line, both guards wake, scout 1 and both guards fall, and scout 2 survives with 20 health.

## Verification

- Headless tests cover command rules, route following, a refused command that keeps the old order, routes around idle units, head-on units in open ground and in a corridor, orders that always end, solid zones by height, randomized histories from fixed seeds with no overlapping footprints and an identical trace on replay, zero allocations per tick, the weapon and cooldown, wall and deck behavior, friendly fire, target ties, guard waking by sight and alarm, standing down, and removal.
- M14 tests cover every order refusal, `Move` against `Command`, an attack that shoots only its target, routes again on its period and ends with its target, and gives up when the target cannot be reached, an attack-move that never walks with an enemy in range and resumes to its goal, hold fire across orders, a guard leaving and rejoining its AI, box selection, group assignment ties, walls, idle units, unreachable cells and more units than cells, and zero allocations for ticks with orders, selection, and group orders. Randomized order histories from fixed seeds replay identically and their orders end. Restoring a snapshot at five points of six seeded histories, including points with bolts in flight and scouts inside the alarm line, and running on reaches the same states and trace as the uninterrupted run. Every truncation and a list of damaged fields are refused, and `Restore` is fuzzed. A recorded history of group orders, an attack-move, focused attacks, hold fire, and a guard under orders has a pinned trace that `make test-wasm` reproduces under WebAssembly.
- A recorded fight and a recorded crossing in the playground scene, and a recorded 128 against 128 battle, have pinned traces that `make test-wasm` reproduces under WebAssembly. In the crossing, one scout stops on the route of another, which waits touching it for at least 30 ticks and then goes around, and nobody is hurt.
- `make smoke-desktop` and `make smoke-web` play the crossing and then the recorded fight with real input and check rendered pixels in OpenGL and Chromium WebGL, restarting after each. In the crossing they see the second scout held exactly one body length behind the first, with the two bodies drawn side by side and not overlapping, then passing on the far side and standing at its goal.
- `make benchmark-skirmish` measures 128 scouts against 128 guards on a 64 × 64 board on native, Node WebAssembly, and Chromium. See the [measurements](../docs/collision-performance.md#measure-a-crowd-and-a-fight).

## References

[RFC 0014](../rfcs/0014-m13-crowds-authored-collision-and-combat.md), [RFC 0015](../rfcs/0015-m14-text-rts-commands-and-replays.md), [RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md), the [vision specification](vision-m15.md), [`examples/skirmish`](../examples/skirmish/sim.go), the [skirmish scene](../cmd/playground/skirmish.go), and the [benchmark](../internal/skirmishbench/skirmishbench.go).

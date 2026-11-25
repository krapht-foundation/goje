# M15 vision: line of sight and fog of war

This is the accepted behavior for the vision slice of the [M15 RFC](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) and for the skirmish events that the same milestone adds. It covers the engine package [`engine/vision`](../engine/vision/vision.go) and the vision option and events of the [skirmish simulation](skirmish-m13.md), whose existing behavior it does not change. The [vision guide](../docs/vision.md) shows how to use them.

## Scope and ownership

`engine/vision` knows cells and a test for whether a cell blocks sight. It decides what a viewer in a cell can see and keeps count of what a set of viewers sees. What blocks sight, where viewers stand, what a team remembers, and what a game does with the result belong to the game. The package uses only integer arithmetic, so one board gives one result on every target.

## Cast

`vision.Cast(origin grid.Cell, radius int, blocked func(x, y int) bool, visit func(x, y int))` calls `visit` once for each cell that the origin sees.

- **Radius.** A cell at offset `(dx, dy)` from the origin can be seen only if `dx² + dy² ≤ radius²`. A radius above `vision.MaxRadius`, 181, is treated as 181, which reaches every cell of a 128 × 128 board from any corner. With a radius of zero or less, only the origin is visited.
- **Origin.** The origin is always visible and is visited first, whether or not it is blocked.
- **Blocked cells.** A blocked cell stops sight and is itself visible when sight reaches it, as a wall is seen from in front.
- **Outside the grid.** `Cast` does not know the grid. `blocked` must report every cell outside the grid as blocked, and `visit` may then receive such a cell when sight reaches it. `Field` hides both details from its callers.
- **Order.** After the origin, `Cast` scans four quadrants in the order north (decreasing y), south, east, and west. Each quadrant is scanned row by row outward from the origin with Albert Ford's symmetric shadowcasting, recursing into the next row for each run of open cells. The order depends only on the origin, the radius, and the blocked cells.
- **Allocation.** `Cast` allocates nothing. Its recursion is at most `radius` calls deep.

### Line of sight

In a quadrant, a cell has a depth `d ≥ 1` along the quadrant's axis and a column `c` with `|c| ≤ d`: north is `(x + c, y − d)`, south `(x + c, y + d)`, east `(x + d, y + c)`, and west `(x − d, y + c)` for an origin at `(x, y)`. A ray from the center of the origin with slope `m`, `−1 < m < 1`, crosses row `d` at column `m·d` and so crosses the cell of that row whose column is nearest to `m·d`.

- A ray is clear up to depth `D` when every cell it crosses in rows 1 to `D − 1` is open. Rays that pass exactly between two cells of one of those rows are left out, and the closure of the clear rays is taken instead. This makes each row act as a line of cells through their centers, blocked cells as solid segments of that line, and a ray that grazes the corner of a blocked cell clear.
- An open cell at `(D, c)` is visible when the slope `c / D` of its center lies in the closure of the rays that are clear up to depth `D`.
- A blocked cell at `(D, c)` is visible when some ray that is clear up to depth `D` has a slope strictly between `(2c − 1) / (2D)` and `(2c + 1) / (2D)`, the slopes of its two edges.
- A cell on a diagonal, where `|c| = d`, belongs to two quadrants and is visible if either quadrant sees it. It is visited once.

The tests compare `Cast` with a direct implementation of these rules on randomized boards.

### Symmetry

For two open cells `a` and `b` on one board with one radius, `Cast` from `a` visits `b` exactly when `Cast` from `b` visits `a`. A blocked origin sees what an open origin in its cell would see; symmetry is promised only between open cells.

## Field

`vision.NewField(width, height, viewers, radius, blocked)` makes a `Field` for a grid of 1 to 128 cells on each side, viewers numbered from 0 to `viewers − 1`, and a radius of 0 to 181. `blocked` is called only for cells on the grid; cells outside it are blocked. It must keep its answers for as long as the Field is used.

- `Move(viewer, cell)` places a viewer in a cell and reports whether what it sees changed. A viewer already in the cell is left alone, so a still army costs nothing. Otherwise the viewer's old cells are taken away and the cells `Cast` visits from the new cell are added. A cell off the grid removes the viewer.
- `Remove(viewer)` takes a viewer away and reports whether it was placed. `Reset` removes every viewer.
- `Visible(x, y)` reports whether any viewer sees a cell. Cells off the grid are never visible.
- `Cells(viewer)` returns the row-major indices of the cells a viewer sees, in the order `Cast` visited them, until the next change to the Field.
- `NewField` sizes each viewer's list for `min((2r + 1)², width × height)` cells, which bounds every cast, so the Field never allocates afterward. A Field is for one goroutine.

## Skirmish rules

`skirmish.Sim.EnableVision(VisionConfig{Radius, Blocked, GuardsExplore})` turns vision on. It must be called before the first `Step` and only once, the radius must be 1 to 181, and `Blocked` must be nil or hold one entry per cell of the board, row-major; otherwise it returns an error and changes nothing. Vision is off by default, and while it is off the simulation, its trace, and its snapshot are exactly those of the [M13 and M14 contract](skirmish-m13.md). `Blocked` and `GuardsExplore` are M17 additions; left at their zero values, every M15 trace and snapshot is unchanged.

With vision on:

- **Sight.** Each team has a `Field` with one viewer per unit. Wall cells of the level block sight, and so does every cell set in `Blocked` (M17), such as a level's `sight_block` zones; the simulation ORs the two into one blocked test, so a blocked cell casts exactly a wall's shadow, is itself visible like a wall, keeps `Cast` symmetric, and does not block movement. Other zones do not block sight, so a raised deck and a solid zone hide nothing. A living unit views from the cell that holds its center, and a removed unit views nothing. A team sees a unit when it sees the cell that holds that unit's center.
- **When sight changes.** Sight is brought up to date by `EnableVision`, by `Restore`, and by `Step` after the dead are removed and before the trace is folded in. A unit recasts only when its cell changed. Sight therefore describes the board between ticks: the orders given between ticks and every rule of the next tick see where the units stood at the end of the last one, even after they move during that tick.
- **Explored cells.** The scouts' team remembers every cell it has seen since vision was turned on, including the cells in sight at that moment. The set only grows. The guards' team remembers nothing, unless `GuardsExplore` is set (M17), in which case it keeps its own set by the same rule. The scouts' set keeps its place in the trace and the snapshot; the guards' set follows it.
- **Fire.** A unit fires only at an enemy in its team's sight: the nearest enemy in range and in sight for a unit without an attack order, and its target only while the target is in range and in sight for an attacking unit.
- **Attack-move.** An attack-move stops for an enemy only while that enemy is in range and in its team's sight, so it walks past an enemy that a wall hides.
- **Attack.** `Order` refuses an `Attack` whose target is not in the unit's team's sight, in addition to the M14 refusals. While the target is in sight, the attack follows it as in M14, and the unit remembers the cell the target stands in. When the target leaves sight, the unit routes to the cell where it last saw the target, treating idle units as walls, and keeps its counts of blocked ticks and new routes, so the repath rule still ends a blocked route. Standing at the center of that cell with the target still out of sight, or finding no route to it, ends the order and the unit stands idle. If the target comes back into sight first, the attack follows it again.
- **Guards.** A guard that is not alarmed considers only scouts in its team's sight within the leash of 12 tiles, whether it is at its post or hunting, so it wakes when any guard sees a scout near it instead of by the six-tile test, and it stands down when no such scout is left. An alarmed guard hunts the nearest scout at any distance without sight, as in M13. Any guard stops walking for a scout in range only while that scout is in sight; an alarmed guard that is in range of a hidden scout keeps routing toward it.
- **Queries.** `Sim.Visible(team, cell)` reports whether a team sees a cell, and `Sim.ExploredBy(team, cell)` (M17) whether it has seen it; `Sim.Explored(cell)` is `ExploredBy(Scouts, cell)`. Without vision, all report every cell on the board. With vision and without `GuardsExplore`, `ExploredBy(Guards, cell)` is false. Cells off the board, and teams other than `Scouts` and `Guards`, are never visible or explored.

## Trace

With vision on, after the bolts, the trace folds in the explored cells as 64-bit words, row-major with cell `i` at bit `i mod 64` of word `i / 64`, then with `GuardsExplore` the guards' explored cells in the same form (M17), and then, for each unit with an `Attack` order in ascending ID order, its last seen cell. Without vision, none is folded, so every M13 and M14 trace is unchanged.

## Snapshot version 2

A Sim with vision on and every M17 option at its default writes version 2, which is the [version 1 layout](skirmish-m13.md#snapshot-m14) with the version field set to 2 and this section appended after the trigger state. With an M17 option, the same section, with the guards' explored cells after the scouts' when the guards explore, follows the options byte of [version 3](skirmish-m13.md#snapshot-m14).

| Part | Fields |
| --- | --- |
| Radius | `u8` sight radius. |
| Explored cells | `ceil(width × height / 8)` bytes; cell `i`, row-major, is bit `i mod 8` of byte `i / 8`. |
| Last seen cells | For each unit in ascending ID order, `i32` the cell where its attack target was last seen, or −1. |

Sight is not stored: `Restore` recomputes both teams' fields from the restored units, so a restored Sim has the counts of the one that was saved. A Sim with vision off writes and reads only version 1, and one with vision on only version 2, so a snapshot made with the other setting is refused. In addition to the version 1 refusals, `Restore` refuses a version 2 snapshot whose radius differs from the Sim's, whose explored bits name a cell off the board, or whose last seen cell is below −1 or off the board, and leaves the Sim unchanged.

The `Blocked` mask, like the walls, is part of the level and not of the snapshot: `Restore` needs a Sim given the same mask, and does not detect another one (M17).

## Events

`Sim.Events()` returns what happened during the last `Step`, in the order it happened, with vision on or off. Each `Event` has a `Kind`, the `Unit` it concerns, and `At`, the center of that unit in ground tiles at the moment of the event; nothing moves after the events of a tick, so `At` is also where the unit stands after the tick.

| Kind | When | Unit |
| --- | --- | --- |
| `EventBoltFired` | A unit fires, including a bolt that is spent at once on a wall or a unit and one that is dropped because it would start off the board. The nth such event of a tick takes the nth new bolt ID. | The shooter. |
| `EventBoltHit` | A bolt hurts a unit, whether it stops at the unit in flight or starts on it. A bolt stopped by a wall or fading out makes none. | The unit hit. |
| `EventUnitFell` | A unit with no health left is removed. | The unit that fell. |
| `EventAlarm` | The alarm rings, once per simulation. | The scout that entered the alarm zone. |

Events are output. They are not in the trace, a snapshot, or a hash, and reading them changes nothing. The slice is reused by the next `Step`, holds at most `3 × 256 + 512 + 1` events, and never allocates after `New`.

## Verification

- `Cast` is compared with the line-of-sight rules above on 1,600 casts over 400 random boards of up to 16 × 16 cells with radii of 0 to 12, checked for symmetry between every pair of open cells on 150 boards, checked for a fixed order and for no allocation, and fuzzed for failures, repeated cells, agreement with the rules, and symmetry from the origin.
- `Field` is compared with casting every viewer from scratch after each of 8,000 random moves, removals, moves off the grid, and resets, and moving all of 256 viewers on a 64 × 64 grid allocates nothing.
- The skirmish tests cover each rule above: the option's errors and defaults, walls hiding and zones not hiding, no shot and no waking through a wall that range alone would allow, guards waking by their team's sight, alarmed guards hunting without sight, an attack refused out of sight that ends on its last seen cell, and an attack-move passing a hidden enemy. Eight seeded order histories with vision on check after every tick that no footprints overlap, that every bolt was aimed at an enemy in the shooter's team's sight, and that the explored cells only grow and include every cell in the scouts' sight; they replay identically, and once the input stops every order ends. Restoring at five points of four of them continues identically, damaged version 2 snapshots are refused, and a tick with vision on allocates nothing.
- Events are checked for each kind with vision on and off, for positions, for health lost per hit, and for an empty list after a quiet tick, and reading them leaves the trace and snapshot of a run unchanged.
- The M13 and M14 traces of the skirmish tests, the benchmark, and the playground are unchanged. A recorded history with vision on has a pinned trace, order results, and explored count that `make test-wasm` reproduces under WebAssembly.
- M17 tests check that a `Blocked` cell hides from both sides like a wall and does not stop a route, that a mask of the wrong size is refused, that the guards remember cells only with `GuardsExplore` and `Explored` equals `ExploredBy` for the scouts, and, over seeded histories with human guards that explore, that each team's explored set only grows, covers what the team sees, and survives `Restore`. The pinned M15 trace is unchanged.

## Measurements

`make benchmark-skirmish` prints a third report, `"Mode":"vision"`: the squad battle of the orders mode with vision on at a radius of 7. On an Apple M-series development machine (darwin/arm64), six native runs of 600 measured ticks had median ticks of 1.10 to 1.19 milliseconds and worst ticks of 2.6 to 3.6 milliseconds, against 0.95 to 0.98 milliseconds median for the same battle without vision; one further run on a loaded machine had a worst tick of 21 milliseconds, together with 14 milliseconds in the battle mode, which has no vision. Under Node WebAssembly the median was 3.3 milliseconds and the worst 7.6 milliseconds. Native and WebAssembly ended with the same trace, `15294946299312324755`, and with 94 scouts and 62 guards alive. The worst native tick stays under half of a 60 Hz frame, so every viewer recasts on the tick its cell changes; the cheaper update the RFC mentions, recasting every second tick, was not needed.

## References

[RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md), the [skirmish specification](skirmish-m13.md), [`engine/vision`](../engine/vision/vision.go), [`examples/skirmish`](../examples/skirmish/vision.go), the [benchmark](../internal/skirmishbench/skirmishbench.go), and Albert Ford's [symmetric shadowcasting](https://www.albertford.com/shadowcasting/).

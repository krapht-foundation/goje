# RFC 0004: M3 — Data-driven levels and unit commands

**Status:** Implemented and verified for M3. The [level format specification](../specs/level-v1.md), [unit command specification](../specs/unit-commands-m3.md), and [content/runtime decision](../decisions/0004-level-data-and-game-rules.md) define the accepted contracts. The [M2 world-view contract](../specs/world-view-m2.md) continues to govern painting and picking.

## Goal

A developer can edit one level file, validate it without launching a graphical host, and play the same level on desktop and web. The player selects a unit, issues a move command to a ground tile, and sees the unit route around a wall. Changing the wall in the file changes the available route without changing Go code. Reset restores the authored layout and spawns. This is a vertical slice for a future level builder, not a proposal to build that editor in M3.

Use a second courtyard variant in the [reference playground](../specs/playground.md). Keep the M2 overlap patrol and M1 pickup as selectable fixtures so their existing smoke and benchmark workloads remain comparable. The new variant becomes the default once it passes the acceptance checks below.

## Selected direction

| Decision | M3 direction | Alternative and tradeoff |
| --- | --- | --- |
| Level format | Small, versioned Goje JSON document with a ground grid and typed placements. The tool and runtime read the same file. | Adopt an editor format now. That unlocks authoring tools earlier but makes M3 depend on their coordinate, layer, and tileset conventions before we know which features Goje needs. |
| Controls | Primary click selects; secondary click commands the selected unit. | Context-sensitive primary click avoids a new input edge, but makes empty ground ambiguous: move there or clear selection. |
| Routing | Four-way breadth-first search on a bounded, uniform-cost grid. | Adjacent-only moves do not demonstrate routing around a wall. Weighted terrain or A* needs more policy than this scenario requires. |
| Ownership | Goje owns level bytes, validation, and reusable grid routing. The game owns archetypes, command policy, speed, and presentation. | Engine-owned unit types would speed up this demo but prematurely fix rules other games need to control. |

## Level contract

A v1 file contains `version`, `rows`, and `placements`. Rows run along increasing ground Y; column index is ground X. Cell `(x, y)` covers `[x, x+1) × [y, y+1)` in ground tile units. Its center is `(x+0.5, y+0.5)`. Placement coordinates are cell indices, not screen coordinates. For example:

```json
{
  "version": 1,
  "rows": [".....", "..#..", "....."],
  "placements": [
    {"id": 1, "kind": "scout", "x": 0, "y": 1},
    {"id": 2, "kind": "lookout", "x": 4, "y": 0}
  ]
}
```

- `.` is walkable ground; `#` blocks walking and the reference scene renders it as a wall. Elevation in M3 is a visual sprite offset, not a second walkable surface. The game maps `kind` to graphics, interaction bounds, and rules; no art names, paths, URLs, or executable content appear in the level file. The example commands the scout; the stationary lookout occupies its cell and provides another depth reference.
- Support one rectangular nonempty board of at most 128 × 128 cells, a 256 KiB input ceiling, and at most 1,024 placements. IDs are unique positive 32-bit integers; two placements cannot share a cell or spawn on blocked ground. `kind` is a nonempty ASCII identifier of at most 32 bytes, beginning with a lowercase letter and containing only lowercase letters, digits, and underscores. Reject missing fields, unknown or duplicate fields, noninteger IDs and coordinates, malformed rows, trailing data, unsupported versions, and nesting beyond the fixed schema. The reference scene and validator also reject unknown archetypes. A failed load must not enter a partially initialized scene.
- Use a public standard-library-only `level/` package for the data contract and a small public `engine/grid/` package for cell conversion and routing. Neither package opens files or imports a host. The example embeds the same bytes on desktop and Wasm, decodes once at launch, and builds fresh mutable scene state on restart. A reference `cmd/levelcheck` validates a file from disk through the same decoder and reports actionable field or cell errors; it does not write or migrate files.

## Command contract

- Extend the existing input snapshot with a secondary mouse press in logical pixels. Buffer the first valid press for each button independently; movement and release do not replace it. Bars do not enqueue either button, and focus loss clears both. Handle the desktop secondary button and the browser mouse button without opening the context menu on the game canvas; preserve normal context menus elsewhere.
- Restart wins over input. Otherwise, resolve primary selection first using M2 paint order; then interpret a secondary press with the same tick-start camera. Invert the camera and project onto elevation zero, then floor both ground coordinates. Secondary presses target ground, not the hit rectangle of an elevated sprite. They do not change selection. If both buttons arrive before one tick, the selection established by the primary press is used regardless of event arrival order.
- Search cardinal neighbors in a fixed order (`+X`, `+Y`, `−X`, `−Y`); return the first shortest route including the destination and excluding the start. Treat board edges, walls, and the stationary lookout as blocked. No selection, blocked or unreachable destination, or an out-of-board press leaves the current order unchanged. A valid new command replaces the destination. If the unit is between centers, finish the current edge and search from that edge's destination; this avoids snapping or reversing mid-edge. Commanding the next center clears subsequent movement. A goal equal to the current center when stationary is a no-op.
- Advance at a fixed simulation speed, clamping each step to the next tile center. The reference scene displays a marker for the accepted destination until arrival and reuses M2's sort and hit view. Movement never depends on draw cadence or camera pan. Rebuild the ordered view only when an anchor moves. Preallocate bounded search, route, and presentation storage when entering the scene; do not search on idle ticks, and measure worst-case reachable and unreachable commands as well as moving frames.

## Implementation and verification order

| Slice | Exit check |
| --- | --- |
| 1. Level contract and validator | Validate the same fixture through the CLI and runtime decoder. Invalid, unsupported, oversized, and malformed fixtures fail clearly; a bounded fuzz test exercises the decoder. Editing a fixture row changes the decoded walkability. |
| 2. Grid routing | Headless tests cover negative and exact-boundary ground coordinates, wall detours, tied routes, unreachable targets, and a full-capacity board. Report warmed search allocations and worst-case query cost. |
| 3. Secondary press and command | Test both press edges in one tick, focus loss, catch-up, resize and bars, command replacement, restart, and arrival at the exact center. The browser test uses actual mouse buttons, including a right click immediately after selection. |
| 4. Playable level and documentation | Native framebuffer and Chromium smoke show a selected scout detouring around the authored wall, reaching its destination, and returning to spawn on reset. Preserve the M1/M2 smoke paths and the separate-module builds on desktop and Wasm. Document the file and how to try a level edit without changing Go code. |

## Acceptance results

- `levelcheck` validates the authored 9 × 7 courtyard through the same decoder and game-specific spawn rules used by both hosts. The decoder tests cover malformed and oversized inputs; a short fuzz run passed. A headless scene test opens the middle wall tile in a copy of the source and observes the scout take the direct route instead of the detour.
- Native and Node Wasm tests cover grid boundaries, deterministic route ties, unreachable goals, full-board search, press buffering across focus loss and catch-up, command replacement, exact arrival, and restart precedence.
- Native OpenGL framebuffer smoke and Chromium WebGL smoke show selection, the wall bypass, arrival without the destination marker, and reset. The native harness injects simulation snapshots rather than operating-system clicks; Chromium exercises real mouse buttons, letterbox rejection, and canvas-only context-menu suppression.
- Pickup and M2 patrol smoke checks still pass. `make check-example` builds the reference command independently for desktop and Wasm. See the [level guide](../docs/levels.md) for the edit-and-play workflow and the warmed routing and moving-frame measurements on both targets.

## Out of scope

A graphical editor, external asset references, tilesets, streaming, arbitrary layers, save files, multi-unit commands, crowd avoidance, dynamic obstacles, diagonal traversal, weighted terrain, slopes, bridges, and a general entity or scripting API. Revisit them after the first author-to-play loop works.

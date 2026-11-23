# RFC 0005: M4 — Level Builder

**Status:** Implemented and verified for M4. The browser Level Builder extends the [M3 author-to-play loop](0004-m3-levels-and-unit-commands.md#acceptance-results) without changing the [Goje v1 level schema](../specs/level-v1.md).

## Goal

A developer can open the authored courtyard, paint an opening in the wall, move a placement, and save a new level without editing source code. The saved level can be reopened in the editor and played with the unchanged Goje playground on desktop and web. The scout taking the newly opened shortcut is the acceptance scenario.

## Selected direction

| Decision | Recommendation | Alternative and tradeoff |
| --- | --- | --- |
| Editor delivery | A standalone, local-first browser app with a Go/Wasm editing core, modest HTML controls, and a top-down grid. Serve static assets on loopback for development. | A native UI can provide direct filesystem integration but needs a widget and distribution choice. Defer it until the edit-to-play workflow is proven. |
| File workflow | Open an explicitly chosen local file and download a newly encoded copy. Leave the source untouched. | In-place save is convenient but adds browser-specific filesystem permissions and safe overwrite behavior. Do not require it for M4. |
| Editor view | Flat rows and columns; leave isometric playback to the playground. | An isometric editor preview looks like the game, but adds picking and presentation work that is not needed to prove the file workflow. |

## Editing contract

- Open one existing v1 level. Show terrain and placements in their existing cells. A wall or floor brush edits terrain; a placement tool relocates an existing placement and preserves its ID, kind, and position in the array. The editor does not add, delete, or rename placements in M4.
- A wall cannot cover a placement. A placement cannot move onto a wall, outside the board, or into another placement. Reject invalid moves with visible feedback rather than silently repairing data. A brush gesture can apply its valid terrain changes, but it must not move or cover a placement.
- A completed brush gesture or placement move is one undoable action. Canceling a gesture rolls it back. Undo and redo restore exact prior cells and positions. A new edit discards redo. Keep at most 64 transactions and 65,536 change entries across both stacks; drop the oldest complete transactions first. Failed and no-op edits do not enter the history.
- Opening invalid or oversized data leaves the active document and its history unchanged. Ask before replacing a document whose edits have not been exported. Export only validated content. Use a fixed JSON field order and indentation, preserve row and placement order, and make equivalent snapshots produce identical bytes. Treat download handoff as an export, not confirmation that the browser wrote the file to disk.
- Offer labeled controls for open, export, tools, undo, and redo. Provide keyboard access to cell coordinates and editing actions; pointer dragging is a shortcut, not the only way to author a level. Report load and validation errors in visible text, not color alone.

## Ownership and trust boundaries

| Owner | Responsibility |
| --- | --- |
| `level/` | Keep `Decode` as the v1 reader and add one validated encoder. The runtime, CLI, and editor must use the same schema constraints rather than maintain a separate JavaScript validator. |
| `internal/leveleditor/` | Hold the mutable working document, edit rules, and bounded undo/redo history. Test without a window or DOM. The runtime board remains read-only. |
| `cmd/leveleditor/` and `web/leveleditor/` | Build and serve the editor separately from the game host. The browser UI owns display and input, and Go owns content state. No editor code enters the game build. |
| Reference playground and `levelcheck` | Keep the scout/lookout requirements game-owned. The generic editor validates the v1 schema; the reference CLI still checks whether the exported map can be used by this particular game. |

Use the browser's file picker and reject files over the v1 byte limit before reading them into the Wasm bridge. Enforce that limit again in the decoder. Treat names and errors as text, never markup. Do not upload content, follow asset references, grant directory access, or mutate the chosen source. Release browser resources associated with each completed or canceled import or download. Render the editor on changes rather than running a gameplay tick loop.

## Implementation and acceptance plan

| Slice | Exit check |
| --- | --- |
| 1. Validated encoder and edit model | Decode the original courtyard, edit terrain and placements, encode, and decode again without changing placement IDs or reordering the file. Reject blocked edits, exercise undo/redo including branching and bounded eviction, and verify deterministic output. |
| 2. Browser editor | Chromium uses the actual file picker workflow, paints the wall, moves the lookout, exercises keyboard editing and undo/redo, and downloads valid JSON. Loading an invalid file leaves current edits intact. Exercise a full 128 × 128 board to catch UI stalls and report edit and export costs separately. |
| 3. Author-to-play round trip | Reopen the downloaded file in the editor and run `levelcheck`. Use a temporary copy of the reference command and replace its embedded asset with the downloaded file. Build the unchanged Go source for desktop and Wasm. Native framebuffer and Chromium canvas checks show the scout crossing the new opening rather than taking the original detour. Keep M3 smoke coverage intact. |

## Acceptance results

The `level` and `internal/leveleditor` tests cover deterministic encoding, invalid edits, and bounded undo/redo. The [level guide](../docs/levels.md#edit-a-level-in-the-browser) records the edit and export timings for a 128 × 128 level with 1,024 placements.

`make smoke-level-roundtrip` passed on macOS arm64 with Go 1.27.4 and Chromium 159. Chromium opened the courtyard through the file picker, painted cell `(4, 2)` to floor, moved the lookout from `(7, 4)` to `(7, 5)`, exported the level, and reopened the download. `levelcheck` accepted that exact file as a 9 × 7 level with two placements.

The check embedded the download in a temporary copy of the reference command without modifying its Go source or the repository fixture. Native OpenGL back-buffer and Chromium canvas checks observed the relocated lookout, the scout crossing the opening, arrival at `(6, 2)`, and reset to the edited layout. The original detour remains covered by `make smoke-desktop` and `make smoke-web`. See [Play a level edit](../docs/levels.md#play-a-level-edit) for manual and automated reproduction.

## Out of scope

New maps, board resizing, new placement kinds, adding or deleting placements, layers, tilesets, external asset references, live preview, collaboration, autosave, native editor distribution, and a shared UI toolkit for future content apps. M4 does not change the engine's desktop or web delivery targets.

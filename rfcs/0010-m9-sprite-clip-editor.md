# RFC 0010: M9 — Sprite Clip Editor

**Status:** Implemented and verified for M9. The editor builds on [M8 animated sprites](0009-m8-animated-sprites.md) without changing the [sprite sheet v1 format](../specs/sprite-sheet-v1.md) or the game-facing runtime API.

## Problem and goal

M8 made sprite playback available to games, but authors still have to calculate source rectangles and pivots and type them into JSON. A developer should be able to open a finished PNG, create or revise clips, preview how the poses line up at a fixed anchor, and download a sidecar that an unchanged game can play.

## Selected direction

| Decision | Selected | Alternative and tradeoff |
| --- | --- | --- |
| Delivery | Ship a standalone, local browser tool using Go/Wasm for the edit model and browser controls for presentation. Reuse the existing editors' file workflow. | Add another mode to the Tile Maker. A shared launcher may make sense later, but tiles and sprite sheets have different data and editing rules. |
| Output | Download only the existing v1 JSON sidecar. Keep the selected PNG unchanged. Reopen an existing pair for further edits. | Invent a separate authoring file or repack sprites on export. Either adds migration or changes an asset the editor is not responsible for painting. |
| Frame editing | Select a rectangle on a magnified sheet, edit exact coordinates and a frame-relative pivot through labeled controls, and set duration in simulation ticks. Use a simple ordered frame list with move-up and move-down actions. | Start with an automatic slicer or a drag-only timeline. Those can speed up regular sheets but do not replace precise, keyboard-accessible editing for irregular frames. |
| Preview | Loop the selected clip with Goje's existing fixed-tick playback logic and a visible ground anchor. Provide pause and single-tick stepping. | Embed a full game or use a second animation implementation in JavaScript. A game introduces launch and input dependencies; a second player can drift from the game runtime. |

## Design

### Editing and preview

- Open a PNG to start with no clips, or open a PNG and an existing v1 sidecar together. Create, rename, select, and delete clips. Add, edit, duplicate, remove, and reorder frames within a clip. A new clip starts with one valid frame; deleting a clip's last frame uses the explicit clip deletion action. A document without clips cannot export.
- Draw source rectangles in sheet pixels with exclusive right and bottom edges. Show pivots relative to their frame, allow edge pivots, and draw a fixed anchor line in the preview. Durations are integer ticks at 60 Hz; milliseconds can be shown as a read-only conversion. The zoom, guides, active selection, and preview transport never change exported data. Keyboard users can edit every field and reorder frames without dragging.
- Treat one confirmed edit as one undoable transaction. Keep at most 64 committed metadata edits across undo and redo; never store copies of the PNG in history. Invalid edits and no-ops leave the document and history unchanged. A valid open that replaces unsaved metadata requires confirmation. Failed or canceled opens preserve the current document. Dirty state follows the serialized metadata relative to the last successful open or download handoff, not selection or preview changes.
- Preview at nearest-neighbor scale against a checkerboard. Use the same frame timing and pivot math as in-game playback. Reset the preview when the selected clip changes or its committed frames change; a paused preview can advance exactly one tick. Stop advancing while hidden rather than replaying elapsed background time. Frame advancement and drawing must not parse metadata, decode the PNG, or mutate the document.

### Ownership, compatibility, and file safety

- Keep the v1 PNG and JSON limits and the same validation rules for the editor, `spritecheck`, and games. Reuse the bounded PNG reader and ordered metadata validation in a repository-internal layer where needed; do not introduce another parser or expose editor state through `sprite`. Preserve clip and frame order in the edit model. The encoder emits explicit v1 fields in a fixed order and produces the same JSON bytes from equivalent ordered metadata within a build.
- Choose each file explicitly. Check encoded sizes before copying bytes into Wasm, then validate the staged input in Go before replacing the current document. Discard a file read if the user has started a newer open. Show filenames and errors as text; release temporary download resources. The tool does not fetch asset URLs, upload content, overwrite the selected files, or modify a project manifest.
- Keep preview changes out of the game runtime. Reuse its existing tick and composition logic without decoding the image again on each edit; load the PNG once per successful open. Play exported metadata in scratch copies of the examples so smoke coverage does not change the canonical M8 asset pair.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Shared format and edit model | Round-trip the M8 asset without changing ordered clip names, rectangles, pivots, or durations. Verify deterministic output, bounded imports and history, canceled/invalid replacements, undo/redo branching, and rejection of edits that would violate v1. Keep existing runtime and validator tests passing. |
| 2. Browser editor and preview | In Chromium, open the PNG and sidecar, edit the hop duration and reorder frames with real input, check pause and one-tick stepping, and observe that changing poses stay on the displayed anchor. Download and reopen the exact JSON against the untouched PNG and verify the edited sequence. Exercise keyboard-only edits and unsaved-work protection. |
| 3. Export-to-game round trip | Pass the exact download and original PNG to `spritecheck`. Embed that pair in scratch builds of both Beacon and the authored courtyard. On desktop OpenGL and Chromium WebGL, check the hop is still airborne on a tick where the original clip would have landed, then lands at the new boundary and resets. Document how to run the editor and reproduce the workflow. |

The first slice is implemented in the [headless edit model](../internal/spriteeditor/document.go), backed by the same format checks as the runtime and `spritecheck`. The M8 sprite pair round-trips in native and Wasm tests, and the existing Beacon smoke exercises the shared runtime on desktop and in Chromium.

The second slice is implemented in the [browser Sprite Clip Editor](../docs/playground.md#author-sprite-clips-in-the-browser). `make smoke-sprite-editor` edits and reorders frames through the UI, checks the edited tick boundary and rendered anchor, tests replacement safety, and reopens the exact download.

`make smoke-sprite-roundtrip` completes the third slice. It validates that exact download with `spritecheck`, embeds it with the unchanged PNG in scratch builds of both games, and checks rendered pixels on desktop OpenGL and Chromium WebGL. The edited pose remains airborne on motion tick 13, where the original would land, and on tick 16. It lands on tick 17 and resets to the original spawn.

## Out of scope

Pixel painting, layers, automatic slicing, atlas packing in the editor, video and audio authoring, importing third-party metadata, runtime hot reload, new clip types, and native editor packaging.

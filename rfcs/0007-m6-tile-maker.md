# RFC 0007: M6 — Browser Tile Maker

**Status:** Implemented and verified for M6. This milestone builds on the [M5 custom ground-art workflow](0006-m5-tile-art.md#acceptance-results). It does not change the [v1 level format](../specs/level-v1.md), tile geometry, or runtime rendering API.

## Goal

A developer can start a tile from a blank canvas or an existing PNG, draw at pixel resolution, and export a playable ground tile without leaving Goje's toolchain. The same PNG reopens in the Tile Maker, passes `tilecheck`, and renders in the courtyard on desktop and web.

## Selected direction

| Decision | Selected | Alternative and tradeoff |
| --- | --- | --- |
| New tile | Start with a blank 32 × 16 transparent canvas. Also support opening a PNG. | Start from a filled diamond template. It is faster for the reference projection, but adds pixels the author did not choose and makes an intentionally transparent tile harder to start. |
| Transparency | Let the pencil paint opaque sRGB colors and the eraser paint transparency. Preserve imported intermediate alpha in untouched pixels. | Add alpha editing now. It supports fades, but makes color picking and transparency controls more complex before the basic workflow is proven. |
| Preview | Show a 1× tile and a small 2 × 2 isometric repeat over a neutral background. Keep the optional diamond guide on the editing grid only. | Embed a playground preview. It would show gameplay context but couples the editor to the runtime and costs a second graphical build to launch. |
| Delivery | Ship a standalone local browser app with Go/Wasm owning pixels and history. Use the Level Builder's file workflow without extracting a shared UI toolkit. | Add an art tab to Level Builder. That removes an app switch, but suggests levels own art references even though the v1 format does not. |

## Editing contract

- Show a magnified grid with pixel coordinates increasing right and down from `(0, 0)`. Checkerboard, pixel grid, and diamond guide are display aids only. The guide connects the midpoints of the 32 × 16 rectangle. It neither clips the pencil nor becomes part of the export.
- Provide labeled pencil, color, eraser, undo, redo, new, open, and export controls. The grid supports arrow-key navigation and **Enter** or **Space** to edit the active pixel. Show the active coordinate and report validation errors in text.
- A pointer stroke applies one color or eraser from press to release, including pixels between successive in-grid samples. Leaving the grid breaks interpolation; returning does not draw across the gap. An interrupted stroke rolls back and creates no history. Keyboard edits are discrete transactions. Ignore no-op edits and discard redo only when a changed transaction commits. Keep at most 64 committed transactions across undo and redo. Because the image is fixed at 512 pixels, whole-image history snapshots have a known upper bound and avoid a second generic history abstraction.
- Ask before New or a valid Open replaces an image with changes since its most recent import, creation, or export. Failed or canceled opens and canceled replacements leave both pixels and history intact. Undoing to the last exported pixels clears the unsaved indicator. Download handoff counts as export, not proof that the file reached disk; never overwrite the chosen source.
- Store editable pixels as straight-alpha 8-bit RGBA. When opening a PNG, normalize supported source formats to that representation. Untouched pixels preserve their normalized color and alpha; do not promise source metadata, 16-bit precision, or identical source bytes. Encode pixels with the Go standard library and no source metadata. Equivalent pixel buffers must produce identical bytes within the same encoder build.
- Display the 1× preview at native tile size. Lay out the 2 × 2 preview with 16 px horizontal and 8 px vertical shifts along each ground axis, with the directions matching the courtyard projection. Use nearest-neighbor magnification if the preview is enlarged. Preview does not need a simulation or graphics host.

## Ownership and file safety

Move the fixed-size PNG contract into a shared toolchain package used directly by `tilecheck`, the reference playground, and the Tile Maker. Migrate the existing readers rather than maintaining two decoders. Share the size limits with the browser bridge. Keep the encoder in that same package; editable state and history belong to the Tile Maker's internal package, not to game code.

Use the existing M5 limits: exactly 32 × 16 pixels and at most 64 KiB encoded. The browser checks the selected file size before copying bytes to Wasm. The Go decoder still enforces both limits and rejects malformed, truncated, and trailing data. Use an explicit file picker and a download, not directory access, URLs, or upload. A stale asynchronous open must not overwrite a newer document. Display filenames and errors as text and release file and download resources when they are no longer needed.

The browser owns controls, input, and presentation. Render the grid and previews on state changes; do not run a continuous game loop. Publish only the current document snapshot to the UI. The game keeps using its existing startup atlas and does not link editor code.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Shared PNG codec and edit model | Verify deterministic encode and reopen, retained intermediate alpha, malformed and oversized file rejection, stroke interpolation and cancellation, history branching and bounds, and unsaved state when undoing to the saved image. Keep M5 inputs valid. |
| 2. Browser authoring | Use real Chromium input to start a blank tile, make asymmetric marks using keyboard and pointer, erase, undo, redo, and toggle the guide. Check the edit grid and repeat preview. Verify canceled replacements and failed imports preserve work. Download and reopen the PNG, then check that the exported pixels match the intended image without guide or preview pixels. |
| 3. Art-to-play round trip | Run `tilecheck` on the exact downloaded file. Embed the PNG and the Level Builder's edited courtyard export in a scratch copy of the reference command. Observe the authored pixels and transparency, the scout taking the shortcut, arrival, and reset in native OpenGL and Chromium WebGL. Keep the original detour smoke intact and document the manual workflow. |

Report browser stroke and export timing separately on the fixed tile using the actual editor controls. Desktop and web playback must not add a runtime decode, upload, or allocation path beyond M5.

## Acceptance results

The shared [`tile` codec](../tile/png.go) is used by the checker, playground, and [headless edit model](../internal/tilemaker/document.go). The [browser Tile Maker](../docs/playground.md#draw-a-ground-tile-in-the-browser) uses that model through a [Wasm bridge](../cmd/tilemaker/main_js.go). The playground still decodes and uploads the tile during startup through the existing M5 atlas path.

`make smoke-level-roundtrip` passed on macOS arm64 with native OpenGL and Chromium 159 WebGL. The Tile Maker smoke drew from blank, exported and reopened the PNG, and compared its complete decoded pixels with the expected image. `tilecheck` accepted that exact download. The same bytes and the exact validated Level Builder download were embedded in both scratch builds. Both targets showed the authored accent, transparent corners at default and zoomed views, the relocated lookout, shortcut crossing, arrival, and reset. The original wall detour also passed in the ordinary desktop and web smokes, and `tilecheck` still accepted the M5 fixture. See [Play a level edit](../docs/levels.md#play-a-level-edit) for the manual workflow.

The round-trip run measured 200 ms for the scripted Tile Maker stroke, including Chromium input and two animation frames, and 43 ms from initiating the export click to the download event. These are workflow observations, not performance thresholds.

## Out of scope

General-purpose Pixel Art Maker features, arbitrary canvas dimensions, layers, partial-alpha painting, animation, tile sets, autotiling, project manifests, level art references, native editor distribution, live game preview, and runtime changes for larger maps.

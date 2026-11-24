# RFC 0011: M10 — Pixel Art Maker

**Status:** Implemented and verified for M10. The [Pixel Art Maker](../docs/playground.md#draw-pixel-art-in-the-browser) exports standard PNGs that follow [sprite sheet v1](../specs/sprite-sheet-v1.md). The [paint-to-play workflow](../docs/playground.md#paint-animate-and-play-a-sprite) verifies the exact editor downloads in desktop and web games without runtime changes.

## Problem and goal

Goje can animate an existing PNG in the [Sprite Clip Editor](0010-m9-sprite-clip-editor.md), but it cannot paint one outside the [Tile Maker](0007-m6-tile-maker.md)'s fixed 32 × 16 ground tile. A developer should be able to draw a transparent sprite sheet in Goje, describe its animation in the existing editor, and play the exact exported files in a desktop or web game.

## Selected direction

| Decision | Selected | Alternative and tradeoff |
| --- | --- | --- |
| Delivery | Ship a standalone local browser editor. Reuse the Tile Maker's stroke and keyboard editing behavior where it fits, without changing the tile workflow. | Put a sprite mode inside the Tile Maker. One entry point is convenient, but tile dimensions, repeat previews, and sprite-sheet output serve different jobs. |
| Artwork | Start with a transparent rectangular canvas or import a PNG. Use one layer, a small preset palette, a custom opaque color, a pencil, and a transparent eraser. | Start with layers or opacity controls. That would support more styles, but increase storage and editing complexity before the asset handoff is proven. |
| Handoff | Download a standard PNG that fits [sprite sheet v1](../specs/sprite-sheet-v1.md). Add rectangles, pivots, and durations only in the Sprite Clip Editor. | Combine painting and animation in one app with an authoring format. That would reduce tool switching, but duplicate or replace the existing clip workflow. |

## Proposed design

- Each sheet has a fixed width and height for the editing session. Either axis may be 1–512 pixels, matching the sprite limit. Coordinates increase right and down from the top-left pixel. Show a magnified nearest-neighbor grid over a checkerboard, the active pixel coordinate, and a 1× preview. Guides, palette choices, zoom, and selection never enter the PNG. Draw only when the artwork or view changes.
- Support continuous, interpolated pointer strokes and pixel-by-pixel keyboard edits with labeled controls. Leaving the grid breaks interpolation. Canceling a stroke rolls back the whole stroke. Undo and redo treat a stroke or a single keyboard edit as one transaction. A no-op does not create history or discard redo. Record only changed pixels and cap the retained history at 64 transactions and 8 MiB of pixel change data, evicting the oldest committed edits first.
- Store straight-alpha 8-bit RGBA. The pencil paints opaque colors and the eraser clears to transparent. Preserve the normalized color and alpha of untouched pixels on import. Re-encoding does not preserve PNG metadata or any precision above 8 bits. Equivalent pixel buffers encode to the same PNG bytes within the same encoder build.
- Check the 1 MiB encoded limit before copying a selected file into Wasm. Use the shared sprite PNG checks in Go for the byte and dimension limits before decoding pixels. Check the encoded export against the same limit; a large, poorly compressible sheet can exceed it even when its dimensions fit. A rejected export leaves the current document editable and unsaved changes intact.
- Stage an import before replacing the current image. Reject an invalid image without touching pixels or history. A valid import or new blank sheet asks before discarding unsaved pixels; a canceled or superseded file read leaves the current document intact. Dirty state compares the artwork to its last successful open, creation, or download handoff. The editor only reads explicitly selected files, never uploads them, and never overwrites the source. Display file names and errors as text and release temporary download resources.
- Go owns pixels, edits, history, and PNG encoding. The browser owns file selection, drawing the editor view, and download handoff. Neither game links editing code or needs a new manifest, PNG format, or runtime upload path. Keep the Tile Maker's fixed dimensions, diamond guide, repeat preview, and output contract unchanged.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Headless artwork editor | Test edge dimensions, bounded malformed imports and oversized exports, alpha preservation, deterministic PNG export and reopen, stroke cancellation, and undo/redo and dirty-state boundaries. Keep existing Tile Maker and sprite format tests passing. |
| 2. Browser Pixel Art Maker | In Chromium, start with a blank non-square canvas. Use keyboard and pointer to paint and erase, exercise undo/redo and replacement protection, and compare the decoded PNG from the actual download with the intended artwork. Reopen the download and verify that no guide or preview pixels were exported. |
| 3. Paint-to-play round trip | Repaint a visible part of the tomato sheet in the Pixel Art Maker, open the exact PNG download in the Sprite Clip Editor, and download an edited v1 sidecar. Pass both downloads to `spritecheck` and embed them in scratch copies of Beacon and the authored courtyard. Check the new paint, transparent pixels, and the edited pose timing with rendered pixels on native OpenGL and Chromium WebGL. Keep the reference assets unchanged and document the workflow. |

Measure edit-to-preview and download handoff separately on a small sheet and a 512 × 512 sheet. Report the observations without setting machine-specific CI timing thresholds.

The first slice is implemented in the [headless artwork editor](../internal/pixelart/document.go). It shares the sprite PNG import limits and the Tile Maker's line interpolation while keeping each editor's history separate. Native and Wasm tests cover the PNG round trip, export limits, and history; the existing Chromium Tile Maker smoke verifies that the shared stroke still behaves the same in that tool.

The second slice adds the [browser Pixel Art Maker](../docs/playground.md#draw-pixel-art-in-the-browser). Its Chromium smoke compares every pixel in a 24 × 12 download against the authored image, reopens that download, and exercises the 512 × 512 limit.

One local run used Chromium 159.0.8154.6, Go 1.27.4, and Node 24.20.0 on macOS arm64. Edit-to-preview measures from the keyboard action until the 1× preview contains the edited pixel. Download handoff measures from initiating the click until Chromium emits the download event, not until the file reaches disk.

| Sheet | Edit to preview | Download handoff |
| --- | ---: | ---: |
| 24 × 12 | 1.4 ms | 40.2 ms |
| 512 × 512 | 1.6 ms | 44.0 ms |

The third slice extends `make smoke-sprite-roundtrip` with the [paint-to-play workflow](../docs/playground.md#paint-animate-and-play-a-sprite). The smoke passes the exact PNG and JSON downloads to `spritecheck`, then embeds both in scratch builds. On macOS OpenGL and Chromium WebGL, Beacon and the courtyard showed the cyan hop stripe and clear space at motion ticks 13 and 16, switched to the unpainted planted pose at tick 17, and restored the standing pose on reset.

## Out of scope

Layers, opacity painting, fill and selection tools, resize and resampling, sprite animation and automatic slicing, atlas packing, tile-set editing, sound and video, project file changes, live game preview, and native editor packaging.

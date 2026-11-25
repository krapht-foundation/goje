# M17 Font Editor

This is the accepted behavior for the font editor slice of the [M17 RFC](../rfcs/0018-m17-versus-music-tools-and-reach.md). The editor reads and writes the [font sheet, version 1](font-v1.md), whose format it does not change. The [text guide](../docs/text-and-ui.md#make-your-own-font) shows how to use it.

## Scope and ownership

The document lives in [`internal/fonteditor`](../internal/fonteditor/document.go). It holds one monospaced font: a cell size, a baseline, a first code point, a glyph count, and the sheet of glyph pixels, with its edit history. The pixel history, stroke rules, and saved state are those of the Pixel Art Maker's [`internal/pixelart`](../internal/pixelart/document.go) document, which it embeds. [`cmd/fonteditor`](../cmd/fonteditor/main_js.go) exposes the document to the page as WebAssembly, [`web/fonteditor`](../web/fonteditor/index.html) is the page, and [`scripts/font-editor-server.mjs`](../scripts/font-editor-server.mjs) serves the built page on loopback with no upload route. Files stay in the browser.

The editor edits glyph pixels only. Proportional advances and kerning are not edited; a font that needs them is written by hand.

## Opening a font

Every font reaches the editor as a `font.Face`, so the editor holds exactly what the runtime would draw.

- **Built-in font.** `fonteditor.Builtin` reads `font.Builtin()`. Its export is the built-in sheet, pixel for pixel, with the description `{"version":1,"cellWidth":6,"cellHeight":8,"baseline":7,"first":32,"count":95}`.
- **A PNG and its JSON.** `fonteditor.Open` passes both readers to `font.Decode`, so every limit and rejection of the format applies unchanged. It then rejects a face that `Face.Monospaced` reports as proportional, because it cannot keep the advances. A face whose `advances` all equal the cell width is monospaced; it opens, and its export omits the redundant list, which draws the same text.
- **A new font.** `fonteditor.New` takes a cell size, a baseline, a first code point, and a count, and checks them by decoding their description against a blank sheet of 512 × 512 pixels. It therefore accepts exactly the monospaced metrics that `font.Decode` accepts, including the largest number of glyphs that fits a sheet.

Opening stages a candidate; a failed open leaves the current font as it was. The page asks before a candidate replaces unsaved changes.

To support this, `font.Face` reports `Cell`, `First`, `Count`, and `Monospaced`, and the package exports its limits as `MaxPNGBytes`, `MaxPNGAxis`, `MaxJSONBytes`, and `MaxCell`. Decoding and drawing do not change.

## Canonical sheet

An opened font is copied into a canonical layout. Glyph `n` is in cell `n`, counting left to right and then top to bottom, as the format requires. A row holds 16 glyphs, or fewer when the font has fewer, and more when 16 a row would need more than 512 pixels of height: the number of columns is the larger of `min(count, 16)` and `ceil(count / floor(512 / cellHeight))`. The sheet has exactly the rows that the glyphs need. Every font that `font.Decode` accepts fits this layout, because its own sheet held the same cells within 512 pixels on each axis.

- Pixels are kept as straight 8-bit RGBA. A 16-bit source loses its low bits, as in the Pixel Art Maker. Pixels outside glyph cells, in unused cells or in a partial column or row, are not part of the font and are not kept.
- The editor paints opaque white and erases to transparent. Other colors and alpha already in an opened sheet are kept until a stroke changes them.

## Editing

- A stroke edits one glyph. `Begin(glyph, at, erase)` starts it at a pixel of that glyph's cell and fails for a glyph outside the font or a pixel outside the cell. `Extend` interpolates within the cell; a point outside the cell breaks the line, so a stroke never paints a neighboring glyph.
- `Commit` keeps a changed stroke as one undoable edit and discards redo. `Cancel` reverts the live stroke. `Undo` and `Redo` apply whole strokes. History holds the last 64 strokes and at most 8 MiB.
- The font is dirty while any pixel differs from the last opened or exported sheet. Metrics are fixed when a font is opened or created.

## Export

- `ExportPNG` encodes the canonical sheet, and fails during a stroke or when the PNG would exceed `font.MaxPNGBytes`, 256 KiB. The sheet's axes are within 512 pixels by construction. An opened sheet whose pixels compress poorly can exceed the limit; the editor then reports the size and keeps the font.
- `ExportJSON` writes the fields `version`, `cellWidth`, `cellHeight`, `baseline`, `first`, and `count`, in that order, with no whitespace and one final newline. It never writes `advances`.
- Identical fonts give identical bytes. `MarkExported` records the PNG handoff and clears the dirty state.
- An exported pair passes `fontcheck` and draws through `font.Decode` the pixels the editor showed.

## Page

- **Built-in font**, **Open PNG and JSON**, and **New font** open a font; opening needs one PNG and one JSON chosen together. **Download PNG** and **Download JSON** export it. The files are named after the opened PNG with `-edited`, `goje-font` for the built-in font, or `font` for a new one.
- A glyph is chosen by typing its character, with **Previous** and **Next**, or by clicking it in the sheet preview. The glyph is announced by its code point and, when printable, its character.
- The editing grid is one cell with a baseline guide. Pencil paints white and Eraser clears. With the grid focused, the arrow keys move the active pixel and **Enter** or **Space** edits it. **Escape** cancels a drag. Leaving the page with unsaved changes asks first.

## Verification

`go test ./internal/fonteditor ./font` covers opening, the built-in pixels, editing, undo, the layout, and every limit. The smoke `scripts/smoke-font-editor.mjs` edits a glyph by keyboard and pointer in Chromium, exports the pair, reopens it, checks rejections and a new font, runs `fontcheck`, and draws the exported glyphs in a scratch program through `font.Decode`, comparing them with the exported sheet.

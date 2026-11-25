# Goje font sheet, version 1

**Status:** Accepted for M14. See the [M14 RFC](../rfcs/0015-m14-text-rts-commands-and-replays.md) for the text and UI plans.

A font asset consists of one PNG and one UTF-8 JSON document, in the style of the [sprite sheet](sprite-sheet-v1.md). The PNG holds glyphs in equal cells, and the JSON describes the cells and the code points they hold. The game supplies both inputs explicitly; the JSON cannot refer to a file or URL.

```json
{
  "version": 1,
  "cellWidth": 6,
  "cellHeight": 8,
  "baseline": 7,
  "first": 32,
  "count": 95,
  "advances": [3, 2, 4]
}
```

The example's `advances` list is shortened; a real one has `count` entries.

- The PNG is at most 256 KiB encoded and 1–512 pixels on each axis. Its origin is the upper-left texel. Draw glyphs in white on a transparent background, so that a tint gives them any color.
- The JSON is at most 16 KiB including whitespace. The root is one object. Fields are case-sensitive and can appear in any order. Every field except `advances` is required. Unknown, duplicate, and null fields are invalid. Trailing whitespace is allowed; extra values and other trailing data are not. All numbers are JSON integers; decimal and exponent spellings are invalid.
- `version` is the integer `1`.
- `cellWidth` and `cellHeight` are 1–32 pixels.
- `baseline` is the distance in pixels from the top of a cell to the baseline the glyphs stand on, from 0 to `cellHeight`. Drawing does not use it; games use it to align text with other art.
- `first` is the code point of glyph 0, from 0 to U+10FFFF. `count` is the number of glyphs, at least 1, and `first + count − 1` is at most U+10FFFF. Glyph `n` holds code point `first + n`.
- Glyph `n` is in cell `n` of the sheet, counting whole cells left to right and then top to bottom. The sheet has `width / cellWidth` columns and `height / cellHeight` rows, rounding down; pixels to the right of the last whole column or below the last whole row are ignored. `count` must not exceed the number of whole cells.
- `advances`, when present, lists one advance per glyph in glyph order: the distance in pixels from the start of the glyph's cell to the start of the next glyph, from 0 to `cellWidth`. Without it, every glyph advances by `cellWidth`, so the font is monospaced.

## Loading and ownership

`font.Decode` takes the PNG and JSON as separate readers. It bounds both input streams and checks the PNG dimensions and the whole description before it decodes any pixels. It returns the zero `Face` and an error on failure and never returns a partial face. The caller owns opening and closing the inputs.

A `Face` is a read-only value that copies share. `Face.Image` returns the sheet, which callers must not modify. The game packs the sheet into its launch atlas, as it does sprite sheets, and calls `Face.InAtlas` with the sheet's position to get a face that draws from the atlas.

Run `go run ./cmd/fontcheck SHEET.png FONT.json` from the repository root to validate both inputs with the same decoder. It exits with status 0 for a valid asset, 1 for an invalid asset or unreadable file, and 2 for wrong arguments, and it does not rewrite either file.

## Drawing

`Face.Draw(canvas, text, x, y, scale, tint)` draws text in logical pixels.

- The upper-left corner of the first glyph's cell is at (`x`, `y`). Every glyph is one `Canvas.ImageTinted` call that draws its whole cell at `cellWidth × scale` by `cellHeight × scale` logical pixels with nearest-neighbor scaling, multiplied by `tint`.
- `scale` is an integer. Values below 1 draw at scale 1.
- The text is UTF-8, and each rune is one glyph. After each glyph, the pen moves right by the glyph's advance times `scale`.
- `\n` (U+000A) starts a new line: the pen returns to `x` and moves down by `cellHeight × scale`. No other control character is special; each is a glyph or a missing glyph.
- A missing glyph is a rune outside the font's range or an invalid UTF-8 byte; each invalid byte is one missing glyph. A missing glyph draws the font's `?` glyph and uses its advance when the font contains U+003F. Otherwise it draws nothing and advances by `cellWidth × scale`.
- Draw has no wrapping, kerning, shaping, combining, or bidirectional text. It allocates nothing.

`Face.Measure(text, scale)` returns the size that Draw covers with the same rules: the width is the largest sum of advances on any line, and the height is the number of lines times `cellHeight`, both times `scale`. Empty text is one empty line. The width includes the full advance of each line's last glyph. `Face.LineHeight` returns `cellHeight`, and `Face.Baseline` returns `baseline`.

## Built-in font

`font.Builtin()` returns a face that every game can use without an asset. Its glyphs were drawn for Goje and carry no outside license.

- It covers ASCII U+0020 to U+007E, 95 glyphs, monospaced in 6 × 8 cells with `baseline` 7. Each glyph is drawn in white on transparent within the upper-left 5 × 7 pixels of its cell. The empty last column and last row keep neighboring glyphs and lines apart.
- Capitals and digits are seven pixels tall. Lowercase letters are five pixels tall; `g`, `j`, `p`, `q`, and `y` use the bottom two rows for their descenders, so their bowls sit two rows above the baseline.
- The sheet is 96 × 48 pixels, 16 glyphs to a row. It is built once from a table in [`font/builtin.go`](../font/builtin.go), which is the source of truth, and shared by every caller. Characters outside the range draw the `?` glyph.

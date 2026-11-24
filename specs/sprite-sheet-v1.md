# Goje sprite sheet, version 1

**Status:** Accepted for M8. See the [M8 RFC](../rfcs/0009-m8-animated-sprites.md) for playback and delivery plans.

A sprite asset consists of one PNG and one UTF-8 JSON document. The game supplies both inputs explicitly. The JSON cannot refer to a file or URL, and the [v1 project manifest](project-v1.md) does not discover or validate sprite assets.

```json
{
  "version": 1,
  "clips": [
    {
      "name": "idle",
      "frames": [
        { "x": 0, "y": 0, "width": 24, "height": 20, "pivotX": 12, "pivotY": 20, "ticks": 60 }
      ]
    }
  ]
}
```

- The PNG is at most 1 MiB encoded and 1–512 pixels on each axis. Its origin is the upper-left texel. Transparency is permitted.
- The JSON is at most 64 KiB including whitespace. The root is one object. Fields are case-sensitive, required, and can appear in any order. Missing, unknown, duplicate, and null fields are invalid at every level. Trailing whitespace is allowed; extra values and other trailing data are not.
- `version` is the JSON integer `1`. The document contains 1–32 clips and 1–256 frame entries in total. Clip names are unique and case-sensitive, start with a lowercase ASCII letter, and may then contain lowercase ASCII letters, digits, and underscores. Names are 1–32 bytes.
- Each clip contains a nonempty array of frames in playback order. All frame fields are JSON integers; decimal and exponent spellings are invalid. `x` and `y` are nonnegative; `width` and `height` are positive. Rectangles must fit entirely in the PNG, with exclusive right and bottom bounds. Frames may reuse or overlap rectangles.
- `pivotX` and `pivotY` are offsets from the rectangle's upper-left edge. Both are nonnegative and can equal the width or height respectively. A bottom-edge pivot lets a sprite stand on its anchor. `ticks` is 1–3,600 fixed simulation ticks; the [graphical hosts](runtime-m1.md#host-timing-and-input) run 60 ticks per second.

## Loading and ownership

`sprite.Decode` takes the PNG and JSON as separate readers. It bounds both input streams, checks PNG dimensions before decoding pixels, and returns no partial asset on failure. The game can share the decoded sheet across scenes or actors; the image returned by `Sheet.Image` is read-only. The caller owns opening and closing inputs and choosing where to place the image in its launch atlas.

Run `go run ./cmd/spritecheck SHEET.png CLIPS.json` from the repository root to validate both inputs with the same decoder. The command does not rewrite either file. The [tomato fixture](../sprite/testdata/tomato.svg) is adapted from the [Goje mascot](../docs/brand/goje.svg); its [PNG](../sprite/testdata/tomato.png) and [clip metadata](../sprite/testdata/tomato.json) are an example asset pair.

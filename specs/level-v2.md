# Goje level JSON, version 2

The [M13 RFC](../rfcs/0014-m13-crowds-authored-collision-and-combat.md) adds authored collision to levels. Version 2 is the [version 1 format](level-v1.md) plus a required `zones` array. Everything in version 1 about size, rows, coordinates, placements, and rejected input applies to version 2 unchanged, except that the `version` field is `2` and the root has one more required field. [ADR 0004](../decisions/0004-level-data-and-game-rules.md) still describes the boundary: a level says where shapes are, and the game decides what they mean.

## Versions and compatibility

- `level.Decode` accepts versions 1, 2, and [3](level-v3.md). A version 1 board has no zones, and a version 1 document that contains `zones` is rejected with the message "zones need version 2". A version 2 document without `zones` is rejected as missing that field, and one with a `sight` object is rejected with "sight needs version 3". Any other version is rejected with "unsupported level version N (want 1, 2, or 3)".
- `level.Encode` takes terrain rows, placements, and zones. It writes version 1 when there are no zones and version 2 otherwise, so a level that uses no zone is byte-identical to what earlier releases wrote, and tools that only read version 1 keep working with it. `level.EncodeSight` with a radius of 0 gives the same bytes, and with a radius writes version 3.
- Programs that call `level.Decode` need no change to read version 1 files. A game that wants zones reads them from `Board.Zones()`. In a version 2 level a zone of kind `sight_block` has no meaning to the decoder, keeps any role, and `Board.SightBlocked` reports false for it.

## Zones

`zones` holds zero to 1,024 zone objects in file order. Each zone requires these fields:

| Field | Value |
| --- | --- |
| `id` | A unique integer among zones from 1 to 4,294,967,295. Placements have their own ID space. |
| `kind` | A 1–32 byte ASCII identifier matching `[a-z][a-z0-9_]*`, the same shape as a placement kind. The game gives it a meaning, such as `deck`, `door`, or `alarm`. The decoder does not check that a game supports it. |
| `role` | `solid` or `trigger`. A solid blocks bodies whose vertical extent overlaps its own. A trigger only reports overlaps. |
| `x`, `y` | The zero-based column and row of the zone's first cell. |
| `w`, `h` | The width and height in cells, each at least 1. The rectangle `[x, x+w) × [y, y+h)` must lie inside the board. |
| `bottom`, `top` | The vertical extent `[bottom, top)` in whole projected pixels, the unit of `engine.WorldItem.Elevation`, with `0 <= bottom < top <= 1024`. |

Zones are tile-aligned. They may overlap each other, walls, and placements, and a `#` cell stays terrain whatever zones cover it. A zone is data only: the decoder does not move, draw, or enforce anything. Zone objects reject missing, duplicated, and unknown fields, as every other object does.

## Encoder output

Goje's encoder writes the root fields in `version`, `rows`, `placements`, `zones` order and each zone on one line with fields in `id`, `kind`, `role`, `x`, `y`, `w`, `h`, `bottom`, `top` order. Indentation and the final newline are deterministic. Readers do not require that formatting or field order.

## Using zones in a game

`collide.LevelShapes(board, wall)` returns one solid per `#` cell with the vertical extent the game chooses, then one shape per zone, split by role into solids and triggers, and `collide.FromLevel(board, wall)` builds a world from them. A zone keeps its own ID as its shape ID. A wall cell gets the ID `collide.WallID(width, x, y)`, which is above the 32-bit range, so wall and zone IDs never collide. See the [collision specification](collision-m12.md#authored-worlds-m13).

## Editing

The [Level Builder](../docs/levels.md#draw-zones) opens both versions, keeps zones it does not touch, and adds, edits, and deletes zones as undoable actions. Since M14 it also [adds and deletes placements and changes their kind](../docs/levels.md#add-and-remove-placements) as undoable actions, under the placement rules of [version 1](level-v1.md); the format does not change.

## Verification

Unit tests cover every rejection above, byte-for-byte round trips of a version 1 file and a version 2 file, the largest legal level, and the editor core's zone history. A short fuzz run decodes arbitrary input without a panic and checks that whatever decodes re-encodes to a document that decodes to the same zones. `make smoke-zone-roundtrip` authors zones in Chromium and plays the export on desktop and web.

## References

[RFC 0014](../rfcs/0014-m13-crowds-authored-collision-and-combat.md), the [version 1 specification](level-v1.md), and the [`level` package](../level/level.go).

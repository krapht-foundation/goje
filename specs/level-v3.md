# Goje level JSON, version 3

The [M17 RFC](../rfcs/0018-m17-versus-music-tools-and-reach.md#authored-vision) lets a level author control vision. Version 3 is the [version 2 format](level-v2.md) plus a required top-level `sight` object, and it gives the zone kind `sight_block` a meaning. Everything in versions 1 and 2 about size, rows, coordinates, placements, zones, and rejected input applies unchanged, except that the `version` field is `3` and the root has one more required field. As in [ADR 0004](../decisions/0004-level-data-and-game-rules.md), the level states the radius and where sight is blocked, and the game decides how its units see.

## Versions and compatibility

- A document is version 3 if and only if it has a `sight` object. A version 3 document requires `zones`, which may be empty, and `sight`. A version 1 or 2 document with `sight` is rejected with "sight needs version 3", and a version 3 document without it is rejected as missing that field.
- `level.EncodeSight(rows, placements, zones, radius)` writes version 3 when the radius is from 1 to `vision.MaxRadius`. With a radius of 0 it returns exactly what `level.Encode` returns, so a level without a sight object is version 1 or 2 and byte-identical to what earlier releases wrote. Any other radius is rejected.
- Tools that read only versions 1 and 2 reject a version 3 file by its version number rather than misreading it.

## Sight

| Field | Value |
| --- | --- |
| `radius` | Required. The sight radius in cells, an integer from 1 to `vision.MaxRadius` (181), the largest radius [`engine/vision`](vision-m15.md) casts. |

The object rejects missing, duplicated, and unknown fields, as every other object does, and a `null` in its place. `Board.Sight()` returns the radius and `true` for a version 3 level, and `0, false` otherwise.

## Sight blockers

A zone of kind `sight_block` blocks line of sight over its rectangle without blocking movement. In a version 3 level its role must be `trigger`, and any other role is rejected with "a sight_block zone must have role "trigger"", because a solid zone already blocks movement and an author should say which is meant. Its vertical extent follows the version 2 rules and has no meaning for sight. A cell under a blocker keeps its terrain, so a unit can stand in a hedge.

`Board.SightBlocked(x, y)` reports whether a `sight_block` zone covers the cell. It reports false for every cell of a level without a sight object, where the kind is an ordinary game-defined kind, and for a cell outside the board. It visits every zone, so a game builds its blocked mask once when it loads the level. Walls are terrain, not zones; a game that casts shadows from walls combines them with this mask.

## Encoder output

The root fields are written in `version`, `rows`, `placements`, `zones`, `sight` order. The zones array is written even when it is empty, as `"zones": []`, and the sight object is written on one line as `"sight": {"radius": N}`. The other formatting is the version 2 encoder's. Readers do not require that formatting or field order.

## Editing

The [Level Builder](../docs/levels.md#set-what-units-can-see) sets the radius in its **Vision** panel and removes it with a radius of 0, as one undoable action, and its **Sight block preset** fills the zone form with the kind `sight_block`, the role `trigger`, and a height of 0 to 32. Export reports a `sight_block` zone with another role instead of writing it.

## Verification

Unit tests cover every rejection above, the byte-for-byte round trip of a canonical version 3 file, the version 1 and 2 bytes that `EncodeSight` writes with a radius of 0, the largest legal level with a sight object, and the editor core's radius history. The level fuzz test decodes arbitrary input without a panic and re-encodes whatever decodes, sight included, to a document that decodes to the same zones and radius. `node scripts/smoke-versus-editor.mjs EXPORT_PATH`, after `make build-level-editor`, authors the shipped versus level's radius, placements, and blockers from its bare terrain in Chromium and checks that the export equals `cmd/playground/levels/versus.json` byte for byte.

## References

[RFC 0018](../rfcs/0018-m17-versus-music-tools-and-reach.md), the [version 2 specification](level-v2.md), the [vision specification](vision-m15.md), and the [`level` package](../level/level.go).

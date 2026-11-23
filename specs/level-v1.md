# Goje level JSON, version 1

The [M3 RFC](../rfcs/0004-m3-levels-and-unit-commands.md) defines the first author-to-play workflow. [ADR 0004](../decisions/0004-level-data-and-game-rules.md) describes the boundary between level data and game rules.

A document is one UTF-8 JSON object, at most 256 KiB including whitespace. It requires exactly these case-sensitive fields, in any order:

| Field | Value |
| --- | --- |
| `version` | The JSON integer `1`. Other versions are unsupported. |
| `rows` | A rectangular grid of 1–128 strings, each containing 1–128 ASCII cells. `.` is walkable and `#` is blocked. |
| `placements` | An array of zero to 1,024 placement objects. Order in the file is preserved. |

Rows increase along ground Y, columns along ground X, with zero-based indices. Cell `(x, y)` covers `[x, x+1) × [y, y+1)` and its center is `(x+0.5, y+0.5)`. These are ground tiles, not projected pixels.

Each placement requires `id`, `kind`, `x`, and `y`. `id` is a unique integer from 1 to 4,294,967,295. `kind` is a 1–32 byte ASCII identifier matching `[a-z][a-z0-9_]*`. `x` and `y` are integer indices of an unoccupied walkable cell. The level decoder checks the identifier's shape, not whether a game supports it. A game must reject unsupported kinds before scene entry.

Objects reject missing, duplicated, and unknown fields. Nulls, fractional or exponent-form numbers, extra nested structures, and non-whitespace trailing data are invalid. The format has no paths, URLs, embedded assets, or executable content. Loading does not mutate files or enter a scene.

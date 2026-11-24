# Goje project manifest, version 1

A project is a directory containing `goje.json`, one [v1 level](level-v1.md), and one [ground tile PNG](../docs/playground.md#validate-custom-ground-art). The [M7 RFC](../rfcs/0008-m7-game-projects.md) defines how games use these files together.

```json
{
  "version": 1,
  "level": "levels/arena.json",
  "ground": "art/stone.png"
}
```

`goje.json` is a single UTF-8 JSON object of at most 4 KiB, including whitespace. It requires these case-sensitive fields in any order:

| Field | Value |
| --- | --- |
| `version` | The JSON integer `1`. Other integer versions are unsupported; decimal and exponent forms are invalid. |
| `level` | Relative path to the level JSON document. |
| `ground` | Relative path to the ground PNG. |

Missing, duplicate, unknown, and null fields are invalid. Trailing whitespace is allowed; trailing values and other data are not.

Paths are case-sensitive, at most 128 bytes, and relative to the manifest directory. Use `/` between components. Each component starts with a lowercase ASCII letter or digit; the rest may also include `_` and `-`. The final component ends in `.json` for `level` or `.png` for `ground`. Component names, excluding the final extension, cannot be `con`, `prn`, `aux`, `nul`, `com1` through `com9`, or `lpt1` through `lpt9`. Absolute paths, empty and dot segments, backslashes, drive prefixes, and URL syntax are invalid. Readers reject invalid paths rather than normalizing them. Use the exact spelling on disk, including when authoring on a case-insensitive filesystem.

## Loading and ownership

`project.Load` receives an already rooted `fs.FS`, validates the manifest and both paths, then opens the referenced regular files and delegates to the level and tile decoders. The level is limited to 256 KiB. The ground tile is exactly 32 × 16 pixels and limited to 64 KiB. The loader does not enumerate the directory, mutate files, or fetch URLs. It returns no partial project on failure. Placement meaning and scene creation remain the game's responsibility.

The supplied filesystem must enforce its own root boundary. An embedded filesystem is suitable for desktop and web builds. The native `projectcheck PROJECT_DIR` uses `os.Root` for the selected directory: in-root relative symlinks are allowed, while absolute and escaping symlinks are rejected. Validate a stable directory; native validation does not isolate hostile mounts or promise a snapshot while another process changes the tree.

# Goje save format, version 1

The [M14 RFC](../rfcs/0015-m14-text-rts-commands-and-replays.md) adds saved games. A save is a scene snapshot in an envelope that names the game and its content and carries a checksum. This document defines the format written by `save.Encode` and read by `save.Decode`, the `engine.Snapshotter` contract, and the key-value store that hosts provide. The [user guide](../docs/replays.md#save-and-load) shows how a game uses them.

## Scope and ownership

Goje owns the envelope and the store. The game owns the snapshot bytes, which only its own `Restore` reads, and decides when to save and load. A save is not encrypted or signed: the checksum detects damage, not tampering, and `Restore` must treat its input as untrusted.

## Snapshots

A scene may implement `engine.Snapshotter`. `Snapshot` returns bytes that capture the scene's whole state. `Restore` replaces the state with bytes it produced earlier, and rejects bytes it cannot read without changing the scene. Restoring a snapshot and continuing must reach the same state as continuing the run that produced it. Scene wrappers from `engine/replay` expose `Snapshot` and `Restore` exactly when the wrapped scene implements them.

## Byte layout

All integers are unsigned and little-endian. `n` is the game name length and `p` is the payload length.

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 8 | Magic number, the ASCII bytes `GOJESAVE`. |
| 8 | 2 | Version, `1`. |
| 10 | 1 | Game name length `n`, 1–32. |
| 11 | `n` | Game name, with the same shape as in a [replay](replay-v1.md#byte-layout). |
| 11+`n` | 8 | Content check value, as in a replay. |
| 19+`n` | 4 | Payload length `p`, 0–1,048,576. |
| 23+`n` | `p` | Payload: the snapshot. |
| 23+`n`+`p` | 8 | Checksum. |

The checksum is CRC-64/ECMA (the `hash/crc64` ECMA table) over every byte before it. It detects any change to a single byte and any burst of changed bits up to 64 bits long. The file ends after the checksum.

## Rejected input

`save.Decode` returns an error for data that:

- is larger than the largest save, 1,048,639 bytes;
- is shorter than the fixed fields or has a bad magic number;
- has a checksum that does not match, which `Decode` checks before reading any other field;
- has a version other than 1 or a game name that is empty, too long, or has another shape;
- ends before the payload length, or has a payload length over 1 MiB or different from the number of bytes between it and the checksum, which covers truncated and trailing data.

`save.Encode` rejects a game name of another shape and a payload over 1 MiB. The decoded payload aliases the input.

A save made by another game or with other content decodes successfully. The game compares the decoded `replay.Meta` with its own using `Meta.Check`, which returns an error wrapping `replay.ErrContentMismatch` when either the name or the content check value differs.

## Store

`platform.NewStore(game)` opens a key-value store for one game, with the game name rules above.

- Keys are 1–32 bytes: a lowercase ASCII letter followed by lowercase letters, digits, underscores, and hyphens. Values are at most 4 MiB. Other keys and values are rejected without touching the store.
- `Put` replaces a value, `Get` returns a copy or an error wrapping `platform.ErrNotFound`, and `Delete` removes a value and succeeds for a missing key. A failed `Put` leaves the previous value in place. A store is not safe for concurrent use.
- On desktop, each value is the file `<key>.bin` in `goje/<game>` under `os.UserConfigDir()`, for example `~/Library/Application Support/goje/<game>` on macOS and `~/.config/goje/<game>` on Linux. The directory is created with mode `0700`. `Put` writes a temporary file with mode `0600` in the same directory, flushes it, and renames it over the old file, so an interrupted write leaves either the old or the new value. A crash can leave a `*.tmp` file, which the store never reads. `Get` refuses a file over 4 MiB.
- In the browser, each value is the local storage item `goje/<game>/<key>`, holding standard base64 text. Browsers limit local storage, commonly to about 5 million characters per site, so a large value can fail to store; the browser then keeps the previous item and `Put` returns the error. `NewStore` fails when local storage is missing or throws, for example when the user blocks site data. A game that cannot open a store plays without saving.

## Verification

Unit tests cover round trips of an empty, a small, and a 1 MiB payload; every single-byte corruption of every field and every truncation; trailing data; field errors behind a valid checksum; oversized input; and content mismatch. A fuzz test decodes arbitrary input without a panic and checks that whatever decodes re-encodes to the same bytes. The desktop store has tests for a round trip, missing keys, malformed keys and game names, oversized values, file modes, a write that fails in a read-only directory without changing the previous value, and a removed directory. The browser store compiles under `GOOS=js GOARCH=wasm` and cannot run under Node, so its behavior is verified in Chromium rather than by unit tests.

## References

[RFC 0015](../rfcs/0015-m14-text-rts-commands-and-replays.md), the [replay format](replay-v1.md), the [`engine/save` package](../engine/save/save.go), and the [store](../platform/store.go).

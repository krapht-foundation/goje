# Goje replay format, version 1

The [M14 RFC](../rfcs/0015-m14-text-rts-commands-and-replays.md) adds recorded sessions. A replay is the input of every tick of a session, state hashes at fixed points, an optional start state, and the identity of the game content it belongs to. This document defines the binary format written by `replay.Encode` and read by `replay.Decode`, and the recording and playback rules of the [`engine/replay` package](../engine/replay/replay.go). The [user guide](../docs/replays.md) shows how a game uses it.

## Scope and ownership

Goje owns the format, the recording and playback wrappers, and the comparison of checkpoints. The game owns its state, its state hash, its snapshot layout, and its content check value. A replay records input, not output: playback reproduces a session only when the scene is deterministic, as described under [replayable scenes](#replayable-scenes).

## Byte layout

All integers are unsigned and little-endian. A replay is at most 2,097,152 bytes (2 MiB). `n` is the game name length and `s` is the start state length.

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 8 | Magic number, the ASCII bytes `GOJERPLY`. |
| 8 | 2 | Version, `1`. |
| 10 | 1 | Game name length `n`, 1–32. |
| 11 | `n` | Game name: a lowercase ASCII letter followed by lowercase letters, digits, and underscores. |
| 11+`n` | 8 | Content check value. |
| 19+`n` | 2 | Checkpoint interval in ticks, 1–65,535. `replay.Recorder` writes 60. |
| 21+`n` | 4 | Start state length `s`, 0–1,048,576. |
| 25+`n` | `s` | Start state. A length of 0 means the replay has none. |
| 25+`n`+`s` | 4 | Tick count `T`, 0–1,048,576. |
| 29+`n`+`s` | 4 | Run count `R`, 0–`T`. |
| 33+`n`+`s` | varies | `R` runs. |
| after the runs | 4 | Checkpoint count `C`, 0 to ⌊`T` / interval⌋ + 1. |
| after the count | 12·`C` | `C` checkpoints, each a 4-byte tick and an 8-byte state hash. |

The file ends after the last checkpoint.

### Runs

A run is a number of consecutive ticks with the same `engine.Input`. It starts with a 2-byte flag set and a 4-byte tick count of at least 1, followed by the fields that the flags mark as present, in the order of this table.

| Flag bit | Meaning | Field when set |
| --- | --- | --- |
| 0 | `Held` is not empty. | 4 bytes, the key bits. |
| 1 | `Pressed` is not empty. | 4 bytes, the key bits. |
| 2 | The `Pointer` coordinates are present. | 16 bytes, `X` then `Y`. |
| 3 | `Pointer.Inside`. | None. |
| 4 | The `PrimaryPress` coordinates are present. | 16 bytes, `X` then `Y`. |
| 5 | `PrimaryPress.Valid`. | None. |
| 6 | The `SecondaryPress` coordinates are present. | 16 bytes, `X` then `Y`. |
| 7 | `SecondaryPress.Valid`. | None. |
| 8 | The `PrimaryRelease` coordinates are present. | 16 bytes, `X` then `Y`. |
| 9 | `PrimaryRelease.Valid`. | None. |
| 10 | The `SecondaryRelease` coordinates are present. | 16 bytes, `X` then `Y`. |
| 11 | `SecondaryRelease.Valid`. | None. |
| 12 | `PrimaryHeld`. | None. |
| 13 | `SecondaryHeld`. | None. |
| 14 | At least one touch is active (M15). | 1 byte, the count `n` of active touches, 1–5, then `n` entries of 20 bytes each: the 4-byte `ID`, then the 8-byte `X` and `Y`. |
| 15 | At least one gamepad is connected (M15). | 1 byte, the count `m` of connected pads, 1–4, then `m` entries of 9 bytes each: the 1-byte slot, the 2-byte `Buttons`, and one byte each for `LeftX`, `LeftY`, `RightX`, `RightY`, `LeftTrigger`, and `RightTrigger`. |

A coordinate is the 8-byte IEEE 754 bit pattern of the `float64`, so playback reproduces the exact value on every target, including the sign of zero and the payload of a NaN. Coordinates are present exactly when at least one of the two values has a nonzero bit pattern, so a pair of `+0` values is never written and `-0` is. Key bits are the `engine.Keys` values; bits above `Key9`, bit 24, are not defined.

Touches are written in their order in `Input.Touches`, which lists active touches first, and every entry is active, so its `ID` is nonzero; `Active` is not stored. A touch's coordinates are always written, including `+0`. Pads are written in increasing slot order, each slot 0–3 at most once, and every entry is connected; `Connected` is not stored. Stick bytes are the two's complement of the `int8` values. A run whose input has no active touch and no connected pad sets neither bit 14 nor bit 15, so it has exactly the bytes it had before M15 added them.

A run holds at least 6 bytes and at most 232: 94 without touches and pads, plus 101 for five touches and 37 for four pads. When the pointer moves on every tick, a run takes 22 bytes per tick, so the size limit, not the tick limit, ends a long recording.

### Canonical form

Every recording has exactly one encoding. Adjacent runs differ in at least one field, where coordinates are compared by bit pattern. A present field is never zero. Inactive touches and disconnected pads are all zero, and no active touch follows an inactive one, because the format cannot store anything else. Checkpoint ticks are strictly increasing, from 1 to `T`, and each is a multiple of the interval or equal to `T`. `Decode` followed by `Encode` returns the same bytes.

## Rejected input

`replay.Decode` returns an error, and allocates nothing from the counts, for:

- more than 2 MiB of data;
- a bad magic number or a version other than 1;
- a game name that is empty, longer than 32 bytes, or has another shape;
- an interval of 0;
- a start state longer than 1 MiB;
- more than 1,048,576 ticks, more runs than ticks, or more runs than the remaining bytes could hold at 6 bytes each;
- more checkpoints than ⌊`T` / interval⌋ + 1, or than the remaining bytes could hold at 12 bytes each;
- a run with a tick count of 0, unknown key bits, or a present field that is zero;
- a touch list with a count of 0 or more than 5, or with an entry whose `ID` is 0, which is how an inactive touch would read; the count is checked before any entry is read;
- a pad list with a count of 0 or more than 4, or with a slot above 3, a repeated slot, or slots out of increasing order; the count is checked before any entry is read;
- adjacent runs with the same input;
- run tick counts whose sum is not `T`;
- checkpoints out of order, outside 1 to `T`, or off the interval other than at `T`;
- data that ends early, or data after the last checkpoint.

`replay.Encode` applies the same rules to a `Recording` value and also rejects one whose encoding would exceed 2 MiB.

## Recording

- `replay.Record(factory, recorder)` returns a factory whose scenes record `tick.Input` before they call the wrapped scene's `Update`. Restart and Replace transitions are wrapped too, so one recorder covers a session that restarts or changes scenes. A tick is counted from 1 at the start of the recording. Runtime tick numbers are not recorded.
- A checkpoint is the `engine.Hasher` state hash of the scene that ran the tick, taken right after its `Update` and before any transition. The recorder takes one on every tick that is a multiple of 60 and on the last recorded tick. A scene that does not implement `engine.Hasher` contributes no checkpoints.
- The last tick's hash is taken when `Recorder.Finish` is called, or when that scene exits if it exits first. `Finish` ends the recording.
- The recorder stops after the tick that leaves no room for one more run of 232 bytes and two checkpoints within 2 MiB, or after tick 1,048,576, whichever comes first. That tick is then the last tick, its checkpoint is taken at once, and `Recorder.Full` reports true. The game keeps running unrecorded. A finished or full recording therefore always encodes. Before M15 the reserve was one run of 94 bytes, so a recording now stops up to 138 bytes earlier than it did.
- Recording allocates nothing per tick while the session stays within the capacity passed to `replay.NewRecorder`.
- The start state is a snapshot passed to `replay.NewRecorder`, for example the payload of a loaded save. The game is responsible for having restored the same snapshot into the first recorded scene before its first update.

## Playback

- `replay.Play(factory, recording, expected)` refuses a recording that breaks a rule above and, with an error wrapping `replay.ErrContentMismatch`, one whose game name or content check value differs from `expected`.
- After the first playback scene enters, the player calls its `engine.Snapshotter.Restore` with the start state, if there is one. A scene without `Restore`, or a failed `Restore`, makes entering fail.
- Each playback update ignores the live input and passes the next recorded input to the wrapped scene, in the same `engine.Tick` otherwise. Transitions are wrapped as during recording.
- After each update, the player compares the checkpoint for that tick. A tick diverges if the hashes differ, if the recording has a checkpoint but the scene has no state hash, or if the scene has a state hash on an interval tick or the last tick but the recording has no checkpoint. The update then returns a `*replay.DivergenceError` naming the tick, and the runtime stops.
- After the last tick, `Player.Done` reports true, and further updates return without updating the wrapped scene, which keeps its final state.
- `replay.Verify(factory, recording, expected)` plays the recording through `engine.Start` and one `Runtime.Step` per tick without a host, then stops the runtime. It returns nil, the first `*replay.DivergenceError`, the content error, or a scene's error.

## Replayable scenes

A scene replays only if every state it reads comes from `engine.Input` and its own history, from the same start state. It must not read `Tick.Number`, a clock, randomness that is not seeded from its own state, the host, or map iteration order. Floating-point arithmetic on pointer coordinates gives the same result wherever the same Go compiler rules apply; the cross-target replay tests check that it does.

## Content check

`replay.ContentHash(parts...)` is the 64-bit FNV-1a hash of the parts, each preceded by its length as an 8-byte little-endian integer. A game passes the bytes that define its content, such as its level and art, in a fixed order, and uses the result as `Meta.Content` for replays and saves.

## Verification

Unit tests cover round trips of every input field, including touches and pads, and of an empty recording, each rejection above and every truncation of a valid file, the recorder's tick and size limits, recording allocation, restarts and replacements, a final tick that changes scenes, start state restore, content mismatch, and a changed input detected at the next checkpoint. The tests run natively and under WebAssembly and pin the bytes of two recorded sessions: one without touches and pads, whose pinned value is unchanged from M14, and one with them. A fuzz test decodes arbitrary input without a panic and checks that whatever decodes re-encodes to the same bytes.

## Changes

- **M15.** Bits 14 and 15, which version 1 required to be zero, now mark touch and gamepad lists, as [RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md#replays-and-the-new-input) proposed. The version stays 1: every earlier recording, and every recording whose input has no active touch and no connected pad, has the same bytes. A decoder from before M15 rejects a recording that uses the new bits as having unknown flags.

## References

[RFC 0015](../rfcs/0015-m14-text-rts-commands-and-replays.md), the [save format](save-v1.md), the [runtime contract](runtime-m1.md), and the [`engine/replay` package](../engine/replay/replay.go).

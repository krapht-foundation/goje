# Match log, version 1

[`engine/matchlog`](../engine/matchlog/matchlog.go) records the completed command ticks of a [lockstep match](lockstep-m16.md). It does not record local input, presentation, pending commands, network messages, or a reason for ending. The game creates the same simulation from the header and defines what its payloads mean. The [outpost](outpost-m14.md#online-play-m16) is the reference.

## Wire form

All integers are unsigned and little endian. The complete log is at most 2 MiB and 1,048,576 ticks.

| Field | Bytes | Values |
| --- | ---: | --- |
| Magic and version | 4 | `47 4d 4c 01`. |
| Mode | 1 | 0 for co-op, 1 for versus. |
| Flags | 1 | Bit 0 enables tactics in co-op; all other bits must be zero. |
| Players | 1 | 2–4. |
| Game-name length | 1 | 1–32. |
| Content-name length | 1 | 1–32. |
| Content check | 8 | The game's content hash. |
| Completed tick count | 4 | 0–1,048,576. |
| Game name | Variable | `[a-z][a-z0-9_]*`, as in replay metadata. |
| Content name | Variable | `[a-z][a-z0-9-]*`. |
| Tick records | Variable | Empty runs and nonempty ticks, described below. |
| Checkpoints | Variable | One 8-byte hash every 60 ticks and at the final tick if it is not a regular checkpoint. |

Each tick record starts with four bytes. A positive value is the count of consecutive ticks whose players all had empty payloads. It cannot exceed the remaining tick count, and adjacent empty runs are forbidden. Zero represents one tick with at least one payload. For each player in index order, that record holds a two-byte length followed by that many bytes, at most 1,024. Empty payloads inside a nonempty record have length zero. There are no padding bytes, explicit checkpoint tick numbers, or trailing data. The checkpoints start after the last complete record; an empty log has no checkpoints.

## Recording and playing

`Recorder.Append` copies the completed tick's original payloads and takes the resulting simulation hash before `lockstep.Done` releases the frame buffers. Invalid append arguments leave the log unchanged. A recorder that hits a size or tick limit keeps the largest prefix that fit, returns `ErrFull`, and takes no further ticks. Even a prefix contains its last completed checkpoint. `Recorder.Encode` returns the bytes of the current prefix, independent of subsequent appends.

`Decode` checks the header, run and payload lengths, tick count, checkpoint extent, and encoded size before returning a log. It borrows the input bytes; callers must keep them immutable. The decoder neither expands empty runs nor interprets payloads. `Log.Reader` makes an independent cursor. `Next` advances one tick and returns read-only payloads, valid until the next call. `Checkpoint` says whether a hash belongs to that tick. Playback validates and applies the commands in the same order as online play, then compares its simulation hash with the recorded hash when present.

M18 adds recorder operations without changing the wire. `Segment(first, last)` returns a standalone encoding of the ticks following the `first` completed ticks, through `last` inclusive. The start is zero or a regular checkpoint. The end is a regular checkpoint or the current end. It splits empty runs at either boundary and rebases the checkpoint indices; callers supply the state at `first` when starting a nonzero segment. `Rewind(tick)` drops all later ticks and permits new appends at zero or a regular checkpoint. An invalid rewind leaves the recorder unchanged.

The hash detects changed content and divergence; it is not a signature or evidence that the match was played by the people it claims. An imported log has the same trust as a received frame. The game must check the header against local content and reject unknown, malformed, or unauthorized commands before applying a tick.

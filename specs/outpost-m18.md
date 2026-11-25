# Outpost: live watch and rejoin, M18

[RFC 0019](../rfcs/0019-m18-live-matches-and-distribution.md) adds live watch and rejoin to the [online Outpost](outpost-m14.md#online-play-m16). The players use the [versioned relay rooms](relay-m18.md). The [lockstep wire](lockstep-m16.md), [match-log wire](match-log-v1.md), game rules, and older recordings are unchanged. Outpost's existing setup message still names the rules at the first connection.

## Room invitations and input

A host sees a player invitation and a separately generated watch code. The player invitation occupies the second player slot. The watch code occupies the one spectator seat and grants no commands. Spectators may pan and zoom and switch the fog view with **Tab** or the **Fog** button. **Escape**, **Leave**, or the gamepad's Start button leaves live watch or cancels rejoin. A watcher leaving cannot affect the match. An automatic player rejoin retains the player's original slot. A browser reload or process exit loses its resume credential; there is no cross-device or cross-process recovery.

## Checkpoint proofs

The players use identical [lockstep sessions](lockstep-m16.md). In addition, Outpost sends a proof for each regular checkpoint once `Confirmed` reaches that tick: both players have produced the subsequent frame, and the sender has checked the peer's checkpoint hash against its own. The receiver independently checks the proof against its local recorded hash. A contradiction ends the match. `Confirmed` alone does not mean the other player has checked this player's hash.

The proof messages are outside lockstep and use the relay's current epoch. Every message in this table is exactly 13 bytes: a message type, a four-byte little-endian completed tick count, and an eight-byte little-endian state hash. A tick is at most `matchlog.MaxTicks`. Checkpoint zero denotes the constructed initial state. The regular checkpoints are positive multiples of 60; no order that was not applied belongs to the history.

| Type | Meaning |
| --- | --- |
| `0x54` | The next regular checkpoint the sender verified. A completed epoch starts reporting after its starting checkpoint. |
| `0x55` | The sender applied and confirmed a natural final tick, which can fall between regular checkpoints. The receiver checks that its own match ended there with the same hash. |
| `0x56` | The sender's latest individually verified checkpoint, offered when the relay restores the pair. |
| `0x57` | The completed tick and hash the sender has rebuilt and is ready to resume from. |

At a natural result, both players wait for the final proof. The host sends the latest verified history to a connected observer before ending the room, subject to a 900-update drain limit. The observer does not send an acknowledgement. If the recorder filled before the final tick, play can still finish; commands beyond the retained prefix are not streamable or recoverable.

## Recovery

A lost player transport pauses both players. A paused scene draws its last state but accepts no orders. An interrupted frame submission does not stop the scene runtime. When the relay restores the pair, both sides send their offers for that epoch. They choose the earlier offered tick, with no rewind behind their shared verified floor. Each starts from its own initial state or a recent local snapshot checked against its own match log, and applies the recorded commands to the agreed tick. There is no trusted snapshot received from another machine. The ready messages must agree on both tick and hash before either side starts a fresh lockstep session at that point. Each discards old pending inputs, frames, and unverified recorded tail, and records the resulting timeline once. The fresh lockstep clock starts at zero relative to its epoch; proofs, recordings, and diagnostics keep absolute ticks.

The relay holds the room for 30 seconds after a transport loss. The disconnected client retries during that time without logging the resume credential. Rebuild work is bounded per update, and a full or inconsistent history, absent proof, expired reservation, or explicit leave ends the match rather than applying unchecked state. With intact recent local checkpoints, at most a short log suffix is replayed. Outpost allows up to two minutes for the rebuild phase, separate from the transport reservation. After rejoin the game shows that later orders did not carry over.

## Live history

The host sends a standalone match-log segment from zero, then segments of at most 60 ticks appended in order. A segment can end only at a mutually verified checkpoint or the separately proven final tick. The host starts each segment with `0x60`, the absolute start tick as a four-byte little-endian integer, and a four-byte little-endian encoded byte size. One or more `0x61` messages then carry the exact bytes in order, up to 2047 encoded bytes per message. A nonregular ending tick is terminal. Later segments use the same content header and begin exactly at the prior end. They contain only the range of ticks and hashes since that end. The receiver decodes and validates the complete segment, then checks its game, content, ownership, and checkpoint hashes as for an offline replay.

A room has at most one queued segment being sent by the host. It queues at most one segment header and two chunks per update. A watcher retains at most two MiB of encoded data, including assembly and active playback, and at most 256 queued segments. The entire stream is at most `matchlog.MaxTicks` ticks. The watcher receives at most 24 messages per update, applies up to 32 ticks per update during catch-up, and otherwise plays one tick at a time. A watcher that breaches a limit or receives invalid data loses only its own view. Already published ticks are never retracted on rejoin. The view does not advance past the last verified checkpoint, apart from the separately verified final tick. The spectator may see both sides; the fog view is not an anti-cheat boundary.

## Verification

- The match-log tests compare range and rewind encodings against complete recordings, including empty runs that straddle a boundary.
- The live-view input tests check in-order assembly, limits, and terminal short segments.
- A native relay test plays a complete duel and requires both players and a live spectator to reach the same final tick and state.
- `make smoke-online` exercises co-op, an injected desync, and a native host with a Chromium guest and a second browser observer. The smoke cuts the host before the first checkpoint and both players in turn after the watcher joins. It checks both agreed resume ticks and later matching hashes, changes the spectator's fog, leaves the view, and plays the persisted match logs.

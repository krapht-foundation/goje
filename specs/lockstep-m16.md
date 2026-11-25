# M16 lockstep protocol

This is the accepted behavior for the lockstep slice of the [M16 RFC](../rfcs/0017-m16-online-lockstep-play.md). It covers the engine package [`engine/lockstep`](../engine/lockstep/lockstep.go): the messages that players exchange, the rules of a session, desync detection, and the in-memory link used by tests. The [online guide](../docs/online.md) shows how a game uses it.

## Purpose and ownership

Lockstep keeps two to four copies of one deterministic simulation in step. Every player sends a payload for every tick, every copy applies all players' payloads for a tick in player order before stepping, and no copy steps a tick before it has every payload for it. Copies exchange state hashes at checkpoints, so a copy that diverges is found at the first checkpoint after the divergence.

The package owns the message format, the schedule, the ordering and range checks, and the hash comparison. It does no I/O, reads no clock, and knows no game rules. The game owns what a payload means, how it is applied, the simulation and its hash, and what happens when a session ends. A `Link` carries the messages; `netlink.Conn` is the network link and `MemoryNet` the in-memory one.

## Messages

Every message is one `Link` message. The first byte is its type, and integers are unsigned and little-endian. `p` is the sender's player index. A message longer than the largest Frame, 1,036 bytes, or shorter than 2 bytes is a protocol error, as is a message of any other type.

| Type | Message | Layout after the type byte | Size |
| --- | --- | --- | --- |
| 1 | Hello | version (1), game name length `n` (1), game name (`n`), content (8), players (1), `p` (1) | 13 + `n` |
| 2 | Frame | `p` (1), tick (8), payload length `k` (2), payload (`k`) | 12 + `k` |
| 3 | Hash | `p` (1), tick (8), state hash (8) | 18 |
| 4 | Bye | `p` (1) | 2 |

- **Hello** is sent once, by `New`. Its version is `lockstep.Version`, 1. The game name is 1 to 32 bytes, and the content is a value that identifies the match content, such as `replay.ContentHash`.
- **Frame** carries the sender's payload for one tick, 0 to `MaxPayload`, 1,024, bytes. It is sent for every tick from `Delay` on, even when the payload is empty.
- **Hash** carries the sender's state hash for a checkpoint tick.
- **Bye** says the sender is leaving the match.

The length of every message must be exactly what its fields say; a message with missing or trailing bytes is a protocol error.

## Session rules

A session is configured with a game name, a content value, the number of players `N` (2 to 4), the local player index, a delay `D` (0 to 30 ticks), a hash interval `I` (at least 1 tick), and a stall limit (at least 1 call). `New` rejects any other configuration. Every player must use the same `D` and `I`; nothing on the wire checks them.

### Agreement

`New` sends the local Hello. A received Hello must have the local version, game name, content, and player count, and a player index below `N` that is neither the local index nor one already received. Anything else is a protocol error. The session is running, and `Running` reports true, once a Hello from every other player has been received.

### Schedule

- The current tick starts at 0. Ticks 0 to `D − 1` are empty for every player: their payloads are agreed to be empty, and no Frame is sent for them.
- When the session starts running it sends the local Frame for tick `D`, with the payload submitted so far.
- `Ready` returns the payloads of the current tick, indexed by player, once every player's Frame for it is present. The game applies them in player order and steps its simulation, then calls `Done` with its state hash.
- `Done` advances the current tick to `t`. If `t` is a multiple of `I`, it sends the Hash for `t` and compares it. It then sends the local Frame for tick `t + D` with the payload submitted since the previous Frame, and clears that payload.
- A payload submitted while the current tick is `t` is therefore applied at tick `t + D + 1`, except that a payload submitted before the session starts running is applied at tick `D`.
- `Submit` appends to the pending payload. If the pending payload would become longer than `MaxPayload`, `Submit` appends nothing and returns `ErrPayloadTooLarge`.

### Ordering

The link must be reliable and ordered, so the session has no retransmission. A Frame from player `p` must be for exactly that player's next expected tick: `D` for its first Frame, and one more for each Frame after it. A Hash from player `p` must be for exactly that player's next checkpoint: `I` for its first Hash, and `I` more for each after it. A Frame, Hash, or Bye from a player whose Hello has not arrived, from an index of `N` or more, or naming the local player is a protocol error.

Frames and Hashes from a player are accepted after its Hello, even before the local session is running, because that player may have received every Hello first.

### Bounds and ring sizes

Every player stops at a tick until it has every Frame for it, so no player is more than `D + 1` ticks ahead of another. The session relies on that bound and rejects messages that break it:

- A Frame for tick `x` is a protocol error if `x` is `2D + 2` or more ticks past the current tick.
- A Hash for tick `c` is a protocol error if `c` is more than `D + 1` ticks past the current tick.
- A Frame for tick `x` is a protocol error if the sender's Hash for a checkpoint at or before `x − D` has not arrived, because the sender sends that Hash before that Frame.

Frames are copied into a ring of `2D + 2` buffers of `MaxPayload` bytes per player, indexed by tick, and checkpoints into a ring of `2D + 2` slots indexed by checkpoint number. With the bounds above, a slot is never needed again before every player's value in it has been used. The session allocates everything in `New`; `Poll`, `Ready`, `Submit`, and `Done` allocate nothing in steady state.

### Stall

`Ready` counts consecutive calls that find a payload missing while the session is running, and resets the count when it returns the payloads. When the count reaches the stall limit, the session ends with `ErrStalled`. A game that calls `Ready` once per update at 60 updates a second and wants the RFC's ten-second limit sets the limit to 600. The session does not count calls before it is running, so the game decides how long to wait for the other players' Hellos.

## Desync detection

At every checkpoint each player sends the hash that it passed to `Done` for that tick. When the local hash and another player's hash for one checkpoint are both known, in `Done` or in `Poll`, they are compared. A difference ends the session with a `*DesyncError` that names the tick, the local hash, the other player's hash, and that player's index. Because every player compares, every player whose copy differs from another reports the same tick. A divergence while applying tick `t` changes the hash passed to `Done` for tick `t + 1`, so it is reported at the first checkpoint at or after `t + 1`.

There is no repair, retry, or resynchronization.

## Errors and the end of a session

The first error ends the session. `Poll` and `Done` return it on every later call, and `Ready` returns nothing.

| Error | Cause |
| --- | --- |
| `*ProtocolError` | A malformed, unknown, out-of-order, or out-of-range message, or a disagreeing Hello. `Player` is the sender's claimed index, or −1 when the message names none. |
| `*DesyncError` | Two hashes for one checkpoint differ. |
| `ErrStalled` | The stall limit was reached. |
| `ErrPeerLeft` | Another player sent Bye. It is wrapped with the player's index. |
| A wrapped link error | `Send` or `Recv` failed. |

`Leave` sends Bye. It does not end the local session; the game stops using it.

`Confirmed` reports the number of ticks for which every player has sent the frame that follows `Done`. Poll it after advancing the final tick and before calling `Leave` to avoid disconnecting an honest peer that still needs to run that tick. It does not compare game outcomes or make the transport trusted.

## Limits

| Limit | Value |
| --- | --- |
| Players | 2 to 4 |
| Delay | 0 to 30 ticks |
| Payload | 1,024 bytes |
| Message | 1,036 bytes |
| Game name | 1 to 32 bytes |
| Memory | `N · (2D + 2)` KiB of frame buffers, 248 KiB at the largest |

## Trust

Every byte from the link is untrusted: it comes from another machine through a relay. The decoder checks each length and index before using it, copies payloads only into the preallocated buffers, never panics, and turns every violation into a `*ProtocolError` that ends the session. A fuzz test feeds arbitrary messages to a session. The session does not check what a payload means; the game must validate every payload it applies, including that a player commands only what it owns.

## The memory link

`NewMemoryNet(n)` returns `n` connected links, one per player, from `Link(player)`. A message sent on one link reaches every other link, reliably and in order. It is for tests and headless runs and is not safe for concurrent use. It can inject faults:

- `SetLatency(k)` makes each later message visible to a receiver only once that receiver has called `Recv` `k` more times.
- `SetBurst(k)` lets a receiver start receiving only on every `k`-th `Recv` call; it then receives every visible message until a call finds none.
- `Cut(p)` drops every later message from player `p`, as a silent connection loss.

## Not in this package

The package does not open connections, measure time, adapt the delay, predict or roll back, rejoin a match in progress, record or replay online matches, or encrypt anything. It does not decide who owns what or what a payload holds.

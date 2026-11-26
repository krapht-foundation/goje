# Relay, M18: versioned rooms, spectators, and rejoin

## Scope and ownership

This specification adds a versioned room mode, v2, to the [M16 relay](relay-m16.md). A v2 room has two player slots that survive a transport interruption of up to 30 seconds and one read-only spectator seat. The [M18 RFC](../rfcs/0019-m18-live-matches-and-distribution.md) records why. The relay still never reads game messages. The v1 routes `/new` and `/join`, their wire contract, and the `netlink.Dial` behavior for them are unchanged.

The server is [`cmd/relay/relay`](../cmd/relay/relay/v2.go). The client is [`netlink`](../netlink/netlink.go) natively and in the browser. What the host sends a spectator, and how players agree on a checkpoint after a pause, belong to the game and are not defined here.

## Routes

Every route is a `GET` request that upgrades to WebSocket.

| Route | Effect | HTTP errors before the upgrade |
| --- | --- | --- |
| `/v2/new` | Opens a v2 room with the caller as host, slot 0. | 503 when the relay already has `-max-rooms` rooms of either version, or is shutting down. |
| `/v2/join?room=CODE` | Joins room `CODE` as guest, slot 1. The code is not case-sensitive. | 404 when no v2 room has that invitation code or its host has not had its Welcome yet. 409 when the room already has a guest, connected or held. |
| `/v2/watch?room=VIEW` | Takes the room's spectator seat, slot 2. | 404 when no v2 room has that spectator code. 409 when the seat is taken. |
| `/v2/resume` | Takes a held player slot back with its resume token. | 503 when `-max-pending` resumes are already waiting to authenticate, or the relay is shutting down. |

v1 and v2 rooms share the `-max-rooms` cap. Invitation codes of both versions and spectator codes are drawn from one namespace, so no code is ever valid for two purposes at once. A spectator code never grants a player slot, and an invitation code never grants the spectator seat.

## Wire contract

Every message is one binary WebSocket message whose first byte is its type. Integers are 8 bytes, big-endian.

Messages the relay sends:

| Type | Bytes | Meaning |
| --- | --- | --- |
| PeerLeft | `0x03` | The room has ended. The relay closes the connection next. |
| Data | `0x04`, epoch, payload | One message from the other player, sent in that epoch. |
| Welcome | `0x05`, slot, invitation code, spectator code, 32-byte token | The first message to a player, after `/v2/new`, `/v2/join`, or a successful `/v2/resume`. Slot is 0 or 1. |
| Welcome | `0x05`, `0x02`, spectator code | The first message to a spectator. |
| Epoch | `0x06`, epoch | Both players are connected, and a new epoch begins. |
| Paused | `0x07` | The other player's transport was lost, and the current epoch has ended. |
| Watcher | `0x08`, id | A spectator with this id is present; id 0 means the seat is empty. |
| View | `0x09`, payload | One message from the host, sent to the spectator. |
| KeepAlive | `0x0D` | The echo of a client keepalive. |

Messages a client sends:

| Type | Bytes | Who | Meaning |
| --- | --- | --- | --- |
| Data | `0x04`, epoch, payload | Players | A game message for the other player in that epoch. |
| View | `0x09`, id, payload | Host | A message for the spectator with that id. |
| End | `0x0A` | Players | Ends the room for everyone. |
| Resume | `0x0B`, 32-byte token | `/v2/resume` only | The credential. It must be the first message. |
| KeepAlive | `0x0D` | Everyone | Proof of life, which the relay echoes. |

A payload is 1 to `-max-message` bytes, so a client message is at most `-max-message` plus 9 bytes. Anything else from a player, including Resume after authentication, is a protocol violation. A spectator may only send KeepAlive; any other message costs it the seat.

### Epochs

An epoch begins each time both player slots become connected: the first pairing starts epoch 1, and each pairing restored after a loss adds one. The relay sends Epoch to both players under both players' write locks, after which it forwards Data only when its epoch equals the current epoch and both players are connected. Data stamped with another epoch, or sent while the room is paused, is dropped. A player therefore never receives a message of an earlier epoch after an Epoch message, and never receives a message of a later epoch than the last Epoch message it saw.

When a connected player's transport is lost, the epoch ends: the relay sends Paused to the other player if it is connected and forwards nothing until the next Epoch.

### Resume tokens

Each player slot gets its own 32-byte token from `crypto/rand` when its Welcome is sent. The token stays the same for the life of the room, so a client whose resume Welcome is lost can retry. Tokens are presented only as the first message of `/v2/resume`, never in a URL, and the relay never logs them. The relay waits up to 15 seconds for that message.

A resume is answered as follows:

- An unknown token, or one of an ended room, gets PeerLeft and a close.
- A token whose slot is still connected gets a plain close. A connected player is never replaced, and the client may retry within the hold.
- A token whose slot is held gets the slot back: the Welcome, the spectator's Watcher message if one is present, and Epoch to both players when the other player is also connected.

A slot is held when its player is lost, either before or after pairing. Either or both slots may be held at once, and either may be resumed first; a connected player stays connected while the other is held.

### Leaving versus losing

The relay distinguishes a deliberate end from a lost transport:

- The room ends when a player sends End, sends a close frame with status 1000, which `netlink`'s `Close` sends, breaks a limit or the protocol, or when a held slot's hold, the join window, or the idle period expires, or the relay shuts down.
- A player is lost on a network error, on 15 seconds without any message, or on a close frame with any status other than 1000. `netlink` sends status 4000 when it gives up on a relay it lost, and the WebSocket subset sends 1001 when its read fails. A browser that unloads a page sends 1001 too, so its slot is held and then expires, because a refresh is not recoverable.

When a room ends, every player still connected receives PeerLeft, including the player that sent End, and the spectator receives PeerLeft after every View already queued for it. All tokens stop working.

### Spectator

A room has at most one spectator. Each spectator gets a new id, counting from 1 within the room, and the relay sends Watcher with that id to both connected players, and Watcher 0 when it leaves. A View is forwarded only when its id is the current spectator's, so a view meant for an earlier spectator never reaches a later one. Views from the guest are dropped.

Each spectator has its own bounded queue of `-view-queue` messages and its own writer goroutine. The host's reader only queues a View without waiting; when the queue is full, the relay disconnects the spectator instead. A slow or stalled spectator therefore never delays either player. A spectator leaving never affects the room.

### Timers

A v2 room has one timer. Before its first pairing, the room expires after the join window. While both players are connected, it expires after the idle period without forwarded Data. While a slot is held, the room expires `-rejoin` after the earliest loss. KeepAlive does not count as activity for the idle period.

### Keepalives

Every v2 client sends KeepAlive at least every 5 seconds when it has nothing else to send, and the relay echoes it. The relay treats 15 seconds without any message as a lost transport, and `netlink` does the same for 15 seconds without any message from the relay. This detects connections that TCP still believes are open, which matters because a resume to a slot the relay still considers connected is refused.

## Limits and flags

The M16 flags apply to v2 rooms as well. `-rate` counts every client message, including KeepAlive. These flags are new:

| Flag | Default | Behavior |
| --- | --- | --- |
| `-rejoin` | 30s | How long a lost player's slot is held. |
| `-max-pending` | 64 | `/v2/resume` connections that have upgraded but not yet presented a token. |
| `-view-queue` | 64 | Messages waiting for a spectator before it is disconnected. |

## Client API

`netlink.Dial` opens and joins v2 rooms through the v2 routes, and `netlink.Resume(ctx, url, token)` dials `/v2/resume`. A `Conn` reports `Slot`, `Code` (empty for a spectator), `ViewCode`, `ResumeToken` (zero for v1 and spectators), `Epoch` (0 before pairing, while paused, and after the connection ends), and `Watcher` (the current spectator id or 0).

- `Send` stamps the current epoch and drops the message while `Epoch` is 0. `SendEpoch(epoch, msg)` and `RecvEpoch(epoch)` check the epoch atomically with the queue and return `ErrEpochChanged` when it is no longer live, so a lockstep session pinned to an epoch cannot send into, or take messages from, a later one.
- On every pause and every new epoch, the receive queue is emptied, except a message the caller still holds. The writer drops queued Data of an ended epoch before writing it.
- `SendView(id, msg)` is for the host only. At most 16 views, a quarter of the 64-message send queue, may wait at once; beyond that `SendView` returns `ErrQueueFull`, so views never take the room game messages need.
- `EndRoom` queues End; a following `Close` writes it after everything queued before it.
- `ErrLost` marks a lost transport, and retrying `Resume` within the hold may succeed. `ErrPeerLeft`, `ErrProtocol`, `ErrRecvFull`, and `ErrClosed` are final. `ErrQueueFull` and `ErrEpochChanged` concern a single call.
- In the browser, the token is sent from the socket's `open` handler. Natively, it is written before the Welcome is read.

## WebSocket subset changes

`netlink/ws` gained `CloseError`, which `ReadMessage` returns for a close frame and which matches `ErrClosed`, carrying the status code; and `Conn.CloseWith(code)`, which is `Close` with a chosen status.

## Security notes

- Tokens are 256-bit random values, distinct per slot and room, and are the only proof of a slot. The relay compares them by map lookup. Anyone who learns a token while its room is open can take a held slot, so clients keep tokens in memory only and should use `wss://`.
- Unauthenticated resumes are bounded by `-max-pending` and a 15-second deadline. A wrong token costs one connection and reveals only that the token is unknown.
- A spectator code lets anyone watch; it grants no orders and no player slot. What the host sends in views is the game's decision.
- The logs contain room codes, slots, and close reasons, never tokens or message contents.

## Verification

- `go test -race ./cmd/relay/...` checks dropping of Data from other epochs, Paused and the next Epoch around a loss and a resume, refusals for a connected slot, an unknown token, and a malformed credential, the pending-resume cap, the shared room cap, that invitation and spectator codes are not interchangeable, spectator generations and stale view ids, a spectator that sends an order, eviction of a spectator that never reads while the players keep exchanging, End reaching the spectator after its last View and every player, expiry of a held slot, and a host lost before pairing that resumes. It also fixes a v1 race in which a host's first message right after PeerJoined could be dropped.
- `go test -race ./netlink` checks v2 Welcome parsing, epoch transitions, pinned send and receive, held messages, writer dropping of stale Data, the view share of the send queue, spectator restrictions, keepalive silence, a race between epoch changes and a pinned reader, and, against a real relay, pairing, watching, ending, resuming, concurrent resumes of both slots, expiry, and that `Close` ends the room at once.

## References

- [RFC 0019](../rfcs/0019-m18-live-matches-and-distribution.md), the [M16 relay](relay-m16.md), and the [WebSocket subset](websocket-m16.md).

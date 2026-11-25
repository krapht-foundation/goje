# Relay, M16

## Scope and ownership

The relay is the meeting point for online lockstep play. Two players connect out to it, it pairs them by a room code, and it forwards each player's messages to the other unchanged. It never reads them and knows nothing about games. The [M16 RFC](../rfcs/0017-m16-online-lockstep-play.md) records why play uses a relay instead of direct connections.

The server is [`cmd/relay`](../cmd/relay/main.go), with its logic in the package [`cmd/relay/relay`](../cmd/relay/relay/relay.go). Its connections use the [WebSocket subset](websocket-m16.md). The client is [`netlink`](../netlink/netlink.go): `netlink.Dial` natively and in the browser. What the players send each other, the lockstep messages, is defined by the lockstep protocol, not here.

## Contract

### Routes

Both routes are `GET` requests that upgrade to WebSocket.

| Route | Effect | HTTP errors before the upgrade |
| --- | --- | --- |
| `/new` | Opens a room with the caller as host, slot 0. | 503 when the relay already has `-max-rooms` rooms or is shutting down. |
| `/join?room=CODE` | Joins room `CODE` as guest, slot 1. The code is not case-sensitive. | 404 when no such room exists or its host is not connected yet; 409 when the room already has a guest. |

Both also answer the WebSocket handshake errors of the subset, such as 400 for a request that is not an upgrade and 403 for a browser origin outside `-origin`. A join that fails its upgrade frees the guest slot again; a `/new` that fails its upgrade closes the reserved room.

### Wire contract

Every message is one binary WebSocket message. The relay sends four types; the first byte is the type:

| Type | Bytes | Meaning |
| --- | --- | --- |
| Welcome | `0x01`, slot, six ASCII code bytes | The relay's first message on every connection. The slot is 0 for the host and 1 for the guest. |
| PeerJoined | `0x02` | Sent to the host when the guest has joined. |
| PeerLeft | `0x03` | Sent to a player when the other one disconnects. The relay then closes the room and this connection. |
| Data | `0x04`, then the other player's message | One forwarded message, byte for byte. |

A client sends only its own messages, each 1 to `-max-message` bytes of anything. The relay does not inspect them. It forwards them in order, each in one Data message, only after the guest has joined. Messages the host sends earlier have no recipient and are dropped, so a host waits for PeerJoined before it starts talking. On joining, the guest receives its Welcome before any Data, and the host receives PeerJoined before any Data from the guest.

Room codes are six characters from `ABCDEFGHJKMNPQRSTUVWXYZ23456789`, which leaves out I, L, O, 0, and 1, chosen uniformly with `crypto/rand` and checked against the open rooms. There are 31⁶, about 887 million, codes.

### Limits and flags

| Flag | Default | Behavior |
| --- | --- | --- |
| `-addr` | `:8080` | TCP address to listen on. |
| `-max-rooms` | 256 | Rooms open at once, including rooms waiting for a guest. A `/new` beyond it gets 503. |
| `-join-window` | 2m | A room that no guest has joined within this time after its Welcome is closed. |
| `-idle` | 30s | A paired room in which no message has been forwarded, in either direction, for this long is closed. |
| `-rate` | 240 | Messages per second per connection, as a token bucket that holds one second's worth. A message that finds the bucket empty closes the connection. Lockstep sends about 60 frames and a hash or two per second. |
| `-max-message` | 2048 | Largest client message in bytes. A larger one closes the connection with WebSocket status 1009, and an empty one closes it too. Clients built with `netlink` accept forwarded messages of up to 2049 bytes, so a relay with a larger limit needs matching clients. |
| `-origin` | none | Allowed browser origin; repeat the flag for more. Without it every origin is allowed. Programs, which send no `Origin`, are always allowed. |

A room holds two players. Whenever either connection ends, for any reason including a broken limit, the relay sends PeerLeft to the other player if the room was paired and closes both connections. A room closed by a timer or by shutdown is closed without PeerLeft. Each room has one timer for its join window and then its idle period, so expiring a room costs no goroutine.

The HTTP server reads request headers within 5 seconds, bounds them at 8 KiB, and applies 10-second read and write timeouts and a 30-second keep-alive timeout before the upgrade. Upgraded connections clear those deadlines; each write then has the subset's 30-second deadline, and reads have none because the room timers bound idle connections. When a peer stops reading, a forward to it blocks for at most that write deadline and then closes the room.

On SIGINT or SIGTERM the relay stops accepting connections, closes every room with a close frame, and exits. It logs with `log/slog` to standard error: the listening address, rooms opening, pairing, and closing with their code and the reason, and rejected upgrades. It never logs message contents.

## Deployment

The relay speaks plain `ws://`. Over the internet, put it behind a reverse proxy that terminates TLS, so players connect with `wss://`, and that forwards WebSocket frames unchanged. The proxy must:

- Pass the upgrade through: forward `Upgrade` and `Connection` to the relay, keep the connection open after the `101` response, and not buffer frames.
- Not add or negotiate extensions such as `permessage-deflate` with the relay. The relay declines them; a proxy that compresses between itself and the browser must decompress before forwarding.
- Keep idle connections open for at least the relay's `-join-window`, or the host's connection may be cut while it waits for a guest.
- Not cache or rewrite the `/new` and `/join` responses, and preserve the `room` query parameter.

For example, nginx needs `proxy_http_version 1.1`, `proxy_set_header Upgrade $http_upgrade`, `proxy_set_header Connection "upgrade"`, and a `proxy_read_timeout` longer than the join window. With `-origin` set, list the origin of the page that serves the web build, such as `https://play.example`.

## Security notes

- The relay never reads game data and cannot validate it. Each peer treats the other peer and the relay as untrusted, and the lockstep decoder enforces its own limits.
- Anyone who can reach the relay can open rooms, and anyone who knows or guesses a code can join an open room. There is no authentication, no accounts, and no moderation. `-max-rooms`, the join window, the idle period, the rate, and the message size bound what one client can cost, but not what many can. Run a public relay behind a proxy that limits connections per address.
- Room codes are random but short. They keep strangers from joining by accident, not by determined guessing; a room accepts one guest, after which it is full.
- The relay answers pings with pongs, and the rate limit counts only data messages. A client that floods pings costs one pong write per ping, which TCP back-pressure bounds when that client stops reading.
- The logs contain room codes, close reasons, and handshake errors. They contain no message contents and no client addresses, but a room code in a log is enough to join that room while it waits for its guest.

## Verification

- `go test ./cmd/relay/...` starts the relay with `httptest` and checks the Welcome bytes and code alphabet, PeerJoined, 200 messages each way in order with sizes from 1 to 2048 bytes forwarded concurrently, dropping of the host's messages before pairing, PeerLeft for either player leaving, 404, 409, and a failed join that frees the slot, origin rejection, the room cap and its release, the largest message and the empty and oversized messages that close a connection, the rate limit and its refill, join-window and idle expiry including traffic that keeps a room open, 20 rooms at once, shutdown, and that every connection goroutine returns. It also passes with `-race`.
- `go test ./netlink` runs native clients against the relay: Welcome, slot, and code, PeerJoined, round trips of up to `netlink.MaxMessage` bytes, a goodbye queued just before `Close` arriving before PeerLeft, errors after the relay shuts down, `Err` reporting the end of a connection without taking queued messages, and a cancelled `Dial`.
- `node scripts/smoke-ws.mjs` builds the relay and the probe `cmd/relay/internal/wsprobe` natively and as WebAssembly, and starts the relay on a loopback port. In Chromium, a page's own `WebSocket` hosts a room; the native probe joins it through `netlink`, and both send 200 messages up to 2048 bytes and check the other's in order. It repeats this with the WebAssembly probe running `netlink`'s browser implementation in the same page, and then with two browser sockets, after which the guest's 2049-byte message must close it with status 1009 and the host must receive PeerLeft. It prints one `passed:` line. On an Apple M4 Pro with Chromium 159 it passed in under 3 seconds.

## References

- [RFC 0017](../rfcs/0017-m16-online-lockstep-play.md) and the [WebSocket subset](websocket-m16.md).

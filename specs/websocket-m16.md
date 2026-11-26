# WebSocket subset, M16

## Scope and ownership

This contract defines the part of the WebSocket protocol ([RFC 6455](https://www.rfc-editor.org/rfc/rfc6455)) that Goje implements in [`netlink/ws`](../netlink/ws/ws.go) for the relay and for native clients. The [M16 RFC](../rfcs/0017-m16-online-lockstep-play.md) records why the project writes a subset instead of adding a module dependency. Browsers do not use this package: the [browser client](../netlink/dial_js.go) uses the page's own `WebSocket`, which implements the whole protocol and interoperates with this subset.

The package owns the opening handshake, the frame reader and writer, masking, the ping and close handshakes, and their limits. It runs over a `net.Conn` and starts no goroutines. The [relay](relay-m16.md) owns rooms and message meaning, and the [`netlink`](../netlink/netlink.go) client owns queues and reconnection policy, of which there is none.

## Contract

### Server handshake

`ws.Accept(w, r, opts)` upgrades an HTTP request. It accepts the request only when all of these hold, and otherwise answers with the HTTP status shown and returns an error wrapping `ws.ErrProtocol`:

| Rule | Status when broken |
| --- | --- |
| The method is `GET`. | 405 |
| The request is HTTP/1.1 or newer. | 400 |
| `Upgrade` contains the token `websocket`, ignoring case. | 400 |
| `Connection` contains the token `upgrade`, ignoring case. | 400 |
| `Sec-WebSocket-Version` is `13`. | 426, with `Sec-WebSocket-Version: 13` |
| There is exactly one `Sec-WebSocket-Key`, and it is standard base64 of 16 bytes. | 400 |
| When `Options.Origins` is not empty and the request has an `Origin` header, the origin is in the list. | 403 |

A request without an `Origin` header is always allowed, because only browsers send it and an origin check cannot stop any other program. The allow-list stops other web pages from using a relay through their visitors' browsers; it is not authentication.

On success the server hijacks the connection with `http.ResponseController`, clears any deadlines the HTTP server left on it, and writes `101 Switching Protocols` with `Upgrade: websocket`, `Connection: Upgrade`, and `Sec-WebSocket-Accept` set to base64 of the SHA-1 of the key followed by `258EAFA5-E914-47DA-95CA-C5AB0DC85B11`. It offers no extension and no subprotocol, so a browser's `permessage-deflate` offer is declined by omission. Bytes the client sent after its request and that the HTTP server already buffered are read as frames. The size of the request is bounded by the HTTP server's `MaxHeaderBytes`, which the relay sets to 8 KiB.

### Client handshake

`ws.Dial(ctx, url, opts)` accepts `ws://` and `wss://` URLs; the default ports are 80 and 443. It connects with `net.Dialer`, wraps the connection in TLS 1.2 or newer for `wss`, and sends a `GET` for the URL's path and query with a key of 16 bytes from `crypto/rand`. It reads at most 16 KiB while it waits for the response and rejects it unless the status is 101, `Upgrade` contains `websocket`, `Connection` contains `upgrade`, and `Sec-WebSocket-Accept` matches the key. It rejects any response that names an extension or a subprotocol, because it asked for neither. The context and the write timeout both bound connecting and the handshake; the context does not affect the returned connection.

### Frames

A connection accepts exactly these frames:

- A final (FIN set) binary data frame of at most `Options.Limit` bytes, 2048 by default.
- A final ping, pong, or close frame of at most 125 bytes. A close payload is empty or begins with a two-byte status.

Every other frame is a protocol error. The reader checks, in this order and before it reads the payload or allocates anything: the three reserved bits are zero; the opcode is binary, close, ping, or pong, so text (1), continuation (0), and unknown opcodes are rejected; FIN is set, so fragmented messages are rejected; frames to a server are masked and frames to a client are not; the length is in its shortest form (16-bit lengths are at least 126, 64-bit lengths are at least 65536 and have the top bit clear); a control frame is at most 125 bytes and a close frame is not one byte; and a data frame is not longer than the limit.

`ReadMessage(buf)` returns the payload of the next binary frame, read into `buf` when its capacity suffices and into a new slice of the payload's length otherwise. While it waits, it answers each ping with a pong carrying the same payload and skips pongs. It answers a close frame with a close frame carrying the same status, or 1000 when the payload is empty, closes the connection, and returns a `ws.CloseError` that matches `ws.ErrClosed` and exposes the received code.

Any read error closes the connection, after a courtesy close frame with status 1009 for a frame over the limit, 1002 for any other protocol error, and 1001 for a timeout or transport error. A connection is not usable after a read error, because the error may have struck in the middle of a frame.

`WriteMessage(msg)` sends one binary frame with the shortest length form and does not retain `msg`. A client masks each frame with a fresh key from `crypto/rand`. `Options.Limit` does not limit writes: the relay writes messages one byte longer than it accepts.

`Close` sends a close frame with status 1000 and closes the connection without waiting for the answer. It never waits behind a blocked write: when another goroutine is writing, it skips the close frame and closes the connection, which ends that write. The close frame itself has a one-second deadline.

`CloseWith(code)` uses the same deadline and sends the specified code. The [v2 relay](relay-m18.md) uses the code to distinguish a deliberate leave from transport loss.

### Deadlines and concurrency

- Each frame write and the client handshake have a deadline of `Options.WriteTimeout`, 30 seconds by default. The server's `101` response has the same deadline.
- Each `ReadMessage` call has a deadline of `Options.ReadTimeout` from its start when that is positive. When it is zero there is no read deadline, and the owner bounds idle connections some other way: the relay closes rooms from a timer, and the netlink client relies on the relay and on the lockstep session's peer timeout.
- One goroutine may read while another writes. Writes, including the pongs and close frames the reader sends, are serialized by a mutex, and each frame is one `Write` call. `Close` is safe from any goroutine.
- After the write buffer has grown to the largest frame, reading and writing a message with a reused buffer allocate nothing.

## Errors and limits

| Limit | Value |
| --- | --- |
| Data payload | `Options.Limit`, 2048 bytes by default |
| Control payload | 125 bytes |
| Handshake response read by a client | 16 KiB |
| Handshake request read by a server | the HTTP server's `MaxHeaderBytes` |
| Write and handshake deadline | `Options.WriteTimeout`, 30 s by default |
| Read deadline | `Options.ReadTimeout`, none by default |

Rejected on purpose, with the reason:

- **Fragmentation and continuation frames.** Goje's messages fit in one frame, and reassembly would need a second, larger buffer and its own limits.
- **Text frames.** Goje's messages are binary; accepting text would require UTF-8 validation for no use.
- **Extensions, including compression.** Decompression can expand a small frame without bound, and the messages are already small.
- **Subprotocols.** The URL path selects the relay route; nothing else is negotiated.
- **Non-minimal lengths.** Every real implementation uses the shortest form, and accepting only it gives each length one encoding.

## Trust

Every byte from the network is untrusted, including the handshake. The reader never allocates more than the limit for a data frame or 125 bytes for a control frame, checks every length before acting on it, and never panics. A peer that sends nothing is bounded by the read deadline or by its owner; a peer that reads nothing is bounded by the write deadline. The package does not authenticate anyone, and `ws://` is not encrypted: encryption is the job of TLS, either `wss://` from the native client or a reverse proxy in front of the relay.

## Verification

- `go test ./netlink/ws` checks the accept value of the RFC 6455 section 1.3 example, every handshake rejection on both sides including an oversized response, each frame rejection with the close status it sends, every accepted length form at its boundaries (0, 1, 125, 126, 127, 2048, 65535, and 65536 bytes) as client and server, masking and the shortest length encoding on write, the ping, pong, and close exchanges over `net.Pipe`, read and write deadlines, a cancelled `Dial`, `ws://` round trips against an in-process server over loopback, and zero allocations per round trip with reused buffers. The pure tests also pass as WebAssembly with `GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./netlink/ws`; the loopback tests are native only.
- `FuzzReadFrame` feeds arbitrary bytes to client and server readers with a 64-byte limit and requires every message to fit in the caller's buffer and every error to close the connection. `FuzzHandshake` feeds the same bytes to the request check and to the response check. On an Apple M4 Pro each ran for 15 seconds, about 2.8 million inputs, without a failure.
- `node scripts/smoke-ws.mjs` connects Chromium's own `WebSocket` to the relay and exchanges messages with this subset; see the [relay contract](relay-m16.md#verification).

## References

- [RFC 0017](../rfcs/0017-m16-online-lockstep-play.md), [RFC 6455](https://www.rfc-editor.org/rfc/rfc6455), and the [relay contract](relay-m16.md).

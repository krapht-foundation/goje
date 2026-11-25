# RFC 0017: M16 - Online lockstep play

**Status:** Implemented in milestone 16. The delivered milestone differs from this proposal in the ways listed under [Implementation notes and results](#implementation-notes-and-results); where they differ, the specifications are the contract.

## Problem and goal

[M15](0016-m15-audio-mixing-vision-and-platform-reach.md) finished the single-player outpost: it looks, sounds, and controls like a game, runs on desktop and web, and can be packaged. Every milestone since [M12](0013-m12-movement-and-collision.md) has also built the same unusual foundation: the simulation is integer arithmetic that gives the same result on every target, it exposes a state hash, and a match can be rebuilt from its inputs. That foundation has been used for replays and saves. It is exactly what is needed to play one match on two machines, and a real-time strategy game that cannot do that is missing its most common feature.

Three gaps stand in the way.

1. A match is driven by the local player's raw input. A selection box and a camera position are local. Two machines can only share what changes the world: the orders that units receive. The outpost mixes the two, because the session calls the simulation directly from its pointer and key handling.
2. Nothing connects two programs. A browser cannot listen for a connection, so two players, one of whom may be in a browser, need a meeting point. The framework has no network code, no wire format, and no handling of a peer that is slow, absent, or lying.
3. Nothing detects that two copies of a match have drifted apart. The state hash exists, but only a replay compares it, against a recording and not against another live match.

A developer should be able to start a relay, host a match from a desktop build, join it from a browser, and play the outpost together against the guards, with the desktop and the browser reaching the same state tick for tick. If a copy ever diverges, both players must be told which tick it happened at, and the match must stop rather than continue as two different games.

This milestone is the networking item of the [M15 plan](0016-m15-audio-mixing-vision-and-platform-reach.md#next-milestone). The reach and depth items of that plan are not part of it.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Model | Lockstep on commands. Each player's scene turns pointer and key input into commands, which are small orders such as "these units attack that one". Commands are scheduled a fixed number of ticks ahead, every player's commands for a tick are applied in player order, and the simulation advances only when every player's commands for that tick have arrived. | Exchange raw input. It is simpler and is what replays record, but selections, cameras, and menus would then have to be identical on every machine, and a stray pointer move would be network traffic. Rollback netcode hides latency better, but it needs the simulation to save and restore its state every tick, which M13's does not and would pay for in every frame. State synchronization is not deterministic play at all. |
| Topology | A relay server. Both players connect out to it, it pairs them by a room code and forwards their messages unchanged, and it knows nothing about the game. | Direct peer-to-peer. It avoids a server, but a browser cannot listen, so it needs WebRTC and a signaling server anyway, and WebRTC has no usable standard-library client for desktop. |
| Transport | WebSocket, with a small subset written in this repository and no new module dependency. Browsers use their own `WebSocket` through `syscall/js`. Desktop and the relay use a pure Go implementation of the handshake and frames of RFC 6455 that supports what the project's own messages need: binary messages in single unfragmented frames of at most 2 KiB, ping, pong, and close. It rejects fragmentation, extensions, compression, and text frames. | Add the `github.com/coder/websocket` module. It handles the whole protocol and runs natively and in the browser, but it would be the project's first dependency that is not for graphics or audio. A subset avoids that and is small enough to test exhaustively, at the cost of code to maintain. Native TCP for desktop only drops cross-play, which is the point of the milestone. |
| Players | Two players cooperate against the guards. The scouts are split between them by ID, and both see what the scouts' team sees. The simulation, the vision rules, and the guards' behavior stay as they are. | Player against player, with the second player commanding the guards. It is the fuller game, but the guards' AI must be switched off per team, the guards need their own explored set and fog, and the HUD and win rules are mirrored. That is a second large change; it follows once the network layer is trusted. |
| Starting a match | One player hosts, and the relay gives the host a room code. The other player joins by giving the code to the game when it is launched: the `room` query parameter on web, and a flag or environment variable on desktop. The title menu offers Host and Join when a relay is configured. | A lobby with text entry. The engine has no text input, and a lobby is a milestone of its own. A fixed room per relay is simple but allows one match at a time. |
| Delay | Commands take effect six ticks, 100 ms, after they are issued, the same for every player and fixed for the match. A player whose commands have not arrived blocks the match, and the screen says so. | Adaptive delay. It improves feel on bad links, but it needs measurement, an agreement protocol, and a change of delay in the middle of a match. A fixed value can be tuned later without changing the format. |
| Desync | Every 60 ticks each peer sends the state hash of that tick. A mismatch stops both matches with an error that names the tick and both hashes. There is no repair and no reconnect. | Resynchronize from a snapshot. Snapshots exist, but a repair hides determinism bugs that the whole architecture depends on being fixed, and it needs a trusted authority between two equal peers. |
| Trust | Each peer treats the relay and the other peer as untrusted. Messages have fixed limits, commands may name only units that their sender owns, and anything out of order or out of range ends the match. The relay limits rooms, message sizes, and rates. | Trust peers because they are friends. A malformed message from the network then becomes a panic or an unbounded allocation. The decoder is a boundary like the replay and level decoders and is treated as one. |
| Where the code lives | A pure package `engine/lockstep` for the protocol and its state, with no I/O. A package `netlink` for the WebSocket connection and its pure Go subset, and `cmd/relay` for the server. Game rules stay in `examples/skirmish` and the outpost. | Put networking in the platform hosts. Both hosts would then need the same code written twice, and neither could be tested headlessly. |

## Proposed design

### Commands

- `examples/skirmish` gains `Command`: a kind, a list of unit IDs, and either an `Order` or a hold flag. Its kinds are the two ways the outpost changes a unit today, `Sim.OrderGroup` and `Sim.SetHold`. `Sim.Apply(c Command) int` runs one and returns how many units took it, and it is the only call the outpost's scene makes to change units.
- A command has a compact binary form with limits, `Command.Append` and `DecodeCommand`: at most `MaxUnits` IDs, an order kind and goal cell in range, and no bytes left over. The decoder allocates only after checking the counts.
- The outpost session issues a command wherever it now calls the simulation. A seam, `issue(Command)`, is the only difference between modes. In a single-player match it applies the command at once, as before, so every single-player trace and replay is unchanged. In an online match it hands the command to the lockstep layer, which applies it at the scheduled tick.
- Ownership is a rule of the outpost, not of the simulation: scouts with odd IDs belong to the first player and scouts with even IDs to the second. In an online match a player can select only their own scouts, and a received command is filtered to units that its sender owns before it is applied. The simulation keeps accepting any command, so single-player play is unchanged.

### The lockstep package

- `lockstep.Session` is a pure state machine for two to four players. It is created with the player count, the local player index, the delay in ticks, and the hash interval. It reads and writes no network. Its inputs are the messages that arrive and the local payload for the tick, and its outputs are messages to send and, when every player's payload for the current tick is present, the payloads to apply.
- A payload is opaque bytes of at most 1 KiB. The game decides what it holds; the outpost puts commands in it. This keeps the package free of game rules and lets another game reuse it.
- Messages are `Hello`, `Frame`, `Hash`, and `Bye`. `Hello` carries the protocol version, a game name, the content hash of the match from [`replay.ContentHash`](../engine/replay), the player count, and the player's index, and both sides must agree on all but the index before the first frame. A `Frame` is the sender's payload for one tick, sent every tick even when empty, and a `Hash` is the sender's state hash for a checkpoint tick. Every message begins with a type byte and a length, and the largest is 1 KiB plus a small header.
- The transport is assumed to be reliable and ordered, as WebSocket is, so the session has no retransmission. A frame for any tick other than the next expected one from that player is a protocol error. The first `delay` ticks are agreed empty on every side, so the match starts without waiting.
- `Session.Poll` takes arrived messages, `Session.Submit` takes the local payload, `Session.Ready` returns every player's payload for the current tick if they are all present, and `Session.Done(hash)` advances the tick and, at a checkpoint, produces the `Hash` message and compares it with those received. A mismatch returns `*DesyncError` with the tick and the local and remote hashes. A peer that sends nothing for ten seconds ends the session with a timeout error. No call blocks or allocates after the first tick.
- The package has an in-memory `Link` pair for tests that can add delay, bursts, and failure.

### The connection and the relay

- `netlink/ws` is the WebSocket subset, with no I/O of its own beyond a `net.Conn`. It has the client and server handshakes, the `Sec-WebSocket-Accept` computation, a frame reader and writer with masking, and the close handshake. The reader enforces the limits before it allocates: it accepts only a final binary, ping, pong, or close frame, requires the reserved bits to be zero, requires client frames to be masked and server frames not to be, and rejects a payload larger than the connection's limit, control frames longer than 125 bytes, and an oversized or malformed handshake. It answers pings and closes politely, and every read and write has a deadline.
- `netlink.Dial(ctx, url)` connects to the relay and returns a `Conn` that implements `lockstep.Link`. Natively it uses `netlink/ws` over `net/http`'s dialer. In the browser it uses the page's `WebSocket` through `syscall/js`, with binary type `arraybuffer`. Either way it reads on its own goroutine into a bounded queue and never blocks the scene, and a scene polls it from `Update`. Neither host changes.
- `cmd/relay` is a small HTTP server. `/new` pairs a host with a freshly made room and sends it the room code and its player index, and `/join?room=CODE` places a guest in an existing room. Once two players are present the relay forwards each binary message to the other player unchanged and does not read it. The relay has limits: a room holds two players, messages are at most 2 KiB, each connection is rate limited, a room that is idle for 30 seconds or is not joined within two minutes is closed, and the total number of rooms is bounded. Room codes are six characters from an alphabet without look-alikes, chosen with `crypto/rand`. It serves the WebSocket subset over plain HTTP, built on `net/http` with a hijacked connection, and encryption is provided by a reverse proxy, which must forward frames unchanged; the guide says so.
- The relay is a development and small-scale tool. Hosting, accounts, matchmaking, and moderation are not part of this milestone.

### The online outpost

- A configured relay adds Host and Join to the title menu. Host connects, shows the room code and a waiting message, and starts when the guest has joined and the hellos agree. Join connects with the code from the launch parameter. Failures show a notice and return to the title, as save failures do.
- An online match runs the same `outpostSession` with commands routed through the lockstep layer. Each tick the scene reads local input, lets the session turn it into commands and update its own selection, camera, and HUD, submits the commands as the local payload, and then, only when `Ready` returns every player's payload, applies them in player order and steps the simulation. While it waits, the simulation and the match clock do not advance and a message shows.
- The match's hash for synchronization is the simulation's trace, not the session's `StateHash`, which includes the player's own selection and camera. Both players' traces must be equal even though their screens differ.
- Pause, save, load, and replay recording are off in an online match, and the pause menu offers Resume, Sound, and Leave. Leaving sends `Bye`. The other player is told and may leave or watch the guards finish the match.
- A desync, a timeout, a protocol error, or a lost connection stops the match and shows the reason and the tick on a screen that returns to the title.

### Allocation and budget

- Encoding and decoding frames, `Session.Poll`, `Ready`, and `Done` allocate nothing in steady state. Tests with `testing.AllocsPerRun` check them.
- The traffic of a two-player match is two small frames per tick per direction, a few kilobytes per second, and a benchmark reports the cost of the lockstep layer per tick next to the simulation's.

## Implementation and acceptance plan

| Slice | Exit check |
| --- | --- |
| 1. Commands | `Sim.Apply` and the codec are tested for every kind, every limit, and every rejection, and the codec is fuzzed. The outpost issues all changes to units through `issue`. Every M13 to M15 pinned trace, replay, and save is unchanged, which shows that single-player behavior did not move. |
| 2. Lockstep | Tests with the in-memory link cover two, three, and four players, delays from zero to twenty ticks, delayed and bursty delivery, a player that stops, protocol violations, mismatched hellos, and an injected one-bit divergence that is reported at the first checkpoint after it. A fuzz test of the message decoder finds no panic. Randomized runs of two real skirmish simulations driven by random commands through the session end with equal traces. |
| 3. Connection and relay | The WebSocket subset is tested against the examples and accept values of RFC 6455, every rejection above, and a fuzz test of the frame reader and the handshake parser. A Chromium check connects the browser's own `WebSocket` to the relay, which shows that the subset interoperates with a real implementation. Loopback tests start a relay and two `netlink` clients, pair them, pass messages in order, and check each limit: room capacity, message size, rate, idle and unjoined expiry, and the room cap. A client that disconnects is reported to the other. The WebAssembly build of `netlink` compiles and its pure parts are tested under Node. |
| 4. Online outpost | Headless tests run two outpost scenes through the in-memory link and show equal traces over a recorded history, a scene that waits for a slow peer, ownership filtering of a forged command, and a desync that stops both. The pinned single-player traces still pass. |
| 5. Cross-play and release | A smoke starts a relay, then a native outpost as host and a Chromium outpost as guest, plays a scripted co-op match, and checks from their logs that both ended on the same tick with the same hash. A second run injects a divergence and checks that both report it. The smoke runs in CI. Documents and measurements are updated. |

## Out of scope

Player against player, and the second-team AI, fog, and win rules it needs. Replays and saves of online matches, which would record commands instead of input, and joining or rejoining a match in progress. Rollback, adaptive delay, and prediction. Matchmaking, accounts, a room browser, text chat, hosting and operating a relay, and encrypted transport inside the relay. Fragmented messages, extensions, and compression in the WebSocket subset. NAT traversal and WebRTC. More than two players in the outpost, although the package supports four. Windows, Android and iOS applications, signing, and the other reach items of the M15 plan, and the depth items: tactical AI, authored vision in the Level Builder, music, and a font editor.

## Contracts this proposal changes

- [`specs/skirmish-m13.md`](../specs/skirmish-m13.md): `Command`, `Apply`, and the command encoding. Existing behavior is not redefined.
- [`specs/outpost-m14.md`](../specs/outpost-m14.md): the online mode, ownership, and the changed pause menu.
- [`specs/playground.md`](../specs/playground.md): the new smoke, and the relay as a tool.
- No new module dependency. The WebSocket subset and its limits are specified in a new document, because a protocol implementation is a trust boundary.
- [`SECURITY.md`](../SECURITY.md) and the security notes: the relay and the peer message decoder are new trust boundaries.
- New specifications: the lockstep protocol, the WebSocket subset, and the relay.

Each change is stated in the document it affects, not applied silently.

## Next milestone

M17 takes what remains of the M15 plan and what this milestone makes possible. It gets its own RFC.

- **Versus.** The second player commands the guards, with per-team AI, fog, and win rules.
- **Reach.** Windows, Android and iOS wrappers around the web build, signing, and update channels.
- **Depth.** Online replays and rejoin, tactical AI, authored vision in the Level Builder, music, and a font editor.

## Implementation notes and results

### Differences from the proposal

- **Command API.** The functions are `Command.AppendTo` and `DecodeCommands`, which decodes a whole payload of commands, not `Append` and `DecodeCommand`. The decoder also rejects a hold flag other than 0 or 1 and a varint longer than needed, so that every accepted payload has exactly one byte form. `Sim.Apply` returns how many units took the command. The outpost session gained an `issuer` field next to `issue`, which a match sets.
- **Delay.** A payload submitted during tick t is applied at tick t + `Delay` + 1, because the frame for the first tick that can take it is sent after the tick completes. A delay of zero would otherwise deadlock. The outpost uses a `Delay` of 5 so that a command takes effect six ticks, 100 ms, after it was issued, as proposed.
- **Lockstep messages.** The messages have no general length byte, because each is one link message and its type fixes its length. Bye carries the sender's player byte like the other messages. The frame ring holds 2 × `Delay` + 2 slots per player, because a peer may be `Delay` + 1 ticks ahead. A hash more than `Delay` + 1 ticks ahead, and a frame whose checkpoint hash is missing, are rejected so that the checkpoint ring cannot overflow. `Leave` only sends Bye. The stall limit counts calls to `Ready`, not time, and starts when the session runs, so a lobby needs its own timeout, which the relay's join window provides. Nothing on the wire carries `Delay` or `HashInterval`, so a mismatch shows as a stall or protocol error, not as a clear message; both ends of the outpost use the same constants.
- **WebSocket subset.** `ws.Dial` also supports `wss://` through `crypto/tls`, so that a desktop client can reach a relay behind a TLS proxy. It has not been run against a TLS server.
- **Relay.** A room closes after a period without forwarded messages, and one timer per room serves both the join window and the idle period. The host's messages before the guest joins are dropped, so the host waits for PeerJoined. Pings are not counted by the rate limit. The relay writes the host's Welcome while holding its lock so that a fast guest cannot get a 404. Clients accept forwarded messages of up to 2,049 bytes. `netlink.Conn` gained an `Err` method that reports the end of a connection without taking queued messages, which the lobby uses to notice a closed relay while it waits for a guest.
- **Browser page for the interop smoke.** Chromium blocks loopback sockets from pages that Playwright serves itself, so `smoke-ws` serves its page from a loopback Node server. It also runs a WebAssembly probe, which is the only runtime test of the browser client in `netlink`.
- **Online outpost.** A failure of the lobby or of the match shows an end screen with the reason and returns to the title on a key, not a notice. A player who leaves ends the match for the other player, who is not offered to watch the guards finish. A command that names a unit that does not exist, is not a scout, or is not the sender's ends the match as a protocol error, instead of being dropped, because an honest copy never sends one. A scout that fell after the command was issued is still allowed. The pause menu is a menu of Resume, Sound, and Leave over a match that keeps running. The title layout, with its three rows of 16 pixels, is unchanged without a relay, and the Host and Join rows are appended below.
- **Native smoke host.** The native side of `smoke-online` steps the app from a timer and opens no window. A first version used a real window and was unreliable on macOS: when Chromium started, the covered window stopped producing frames, which stalled the guest. The window itself is covered by the other desktop smokes.
- **Hash fault.** A smoke build changes the host's hash from the tick in `GOJE_ONLINE_FAULT`, to prove that a divergence is reported.

### Results

| Check | Result |
| --- | --- |
| Commands | `Apply` matches direct calls on four seeds over 900 ticks with vision off and on. The codec has round trips, one test for each rejection, zero allocations when decoding into reused storage, and a fuzz test that ran 7.8 million executions without a failure. Every M13 to M15 pinned trace, replay, and save is unchanged. |
| Lockstep | Two, three, and four players with delays of 0, 1, 6, 20, and 30 ticks, over reliable, delayed, bursty, and delayed bursty links, end equal. A one-bit change is reported as a desync at the first checkpoint after it on every player. A cut-off player is reported as stalled after exactly the stall limit. 28 protocol violations are rejected. Two real skirmish simulations driven by random commands end with equal traces on four seeds. The message fuzz test ran 8.1 million executions without a failure. A tick for two players costs 148 ns natively and 686 ns as WebAssembly under Node, with no allocation, against 16.7 ms per tick. |
| WebSocket and relay | The subset passes the RFC 6455 examples, every rejection, and two fuzz tests, which ran 2.8 million inputs without a failure. The relay's tests cover pairing, 200 messages each way in order with sizes from 1 to 2,048 bytes, each limit, expiry, shutdown, and goroutine cleanup, and pass with `-race`. `make smoke-ws` exchanges 200 messages each way between a Chromium `WebSocket`, the native client, and the WebAssembly client through the relay, and checks that an oversized message closes its sender. |
| Online outpost | Headless tests run two copies of a match over the in-memory network, with and without latency, and show equal traces on every tick that both ran, selections limited to a player's own scouts, a stall after a player stops, rejection of commands for the other player's scout, for a guard, and for a unit that does not exist, a changed hash stopping both at the first checkpoint, and the launch parameters. `make smoke-online` plays 900 ticks between a native host and the WebAssembly guest in Chromium through a real relay and found fifteen state hashes logged by both, all equal, in each of the four runs after the host stopped using a window. A second run with a changed hash from tick 100 ended both players with the desync. |
| Gate | `make check`, `make test`, `make test-wasm`, `make test-race`, `make vuln`, `make check-example`, lint under the default, WebAssembly, `goje_smoke`, and `goje_smoke,goje_beacon_smoke` settings, `make smoke-desktop`, `make smoke-web`, `make smoke-input`, `make smoke-mixer`, `make smoke-beacon`, `make smoke-goje-build`, `make smoke-outpost-roundtrip`, `make smoke-placement-editor`, `make smoke-ws`, and `make smoke-online` pass on macOS. `go.mod` is unchanged. |

### Not checked

Play across a real network: every run was on one machine over loopback, so latency, loss, and a slow link are tested only through the in-memory network's delays. `wss://` against a real TLS server or reverse proxy, in the browser or natively. Firefox and Safari. Linux and Windows hosts beyond what CI runs. More than one match at a time through one relay beyond the relay's own tests with 20 rooms. A relay left running for days, and its memory use under attack. A browser tab that is in the background, where timers are throttled, so its player would stall the other. A guest on a touch screen or a gamepad. The browser client's back-pressure under a slow network. That a mismatch of `Delay` or `HashInterval` between two builds produces a usable message.

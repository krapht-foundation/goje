# Online play

Goje can run one match on several machines with lockstep: every machine runs the whole simulation, the players exchange only the small payloads that change it, and every copy applies the same payloads at the same tick. The [`engine/lockstep`](../engine/lockstep/lockstep.go) package keeps the copies in step and detects when they drift apart. The [lockstep protocol](../specs/lockstep-m16.md) defines the exact rules.

## What your game needs

Lockstep works only for a simulation that is deterministic: given the same start and the same payloads in the same order, it must reach the same state on every machine and target. The rules for a [replayable scene](replays.md#make-a-scene-replayable) apply, and more strictly, because two different machines must agree, not one machine with itself:

- Use integer or fixed-point arithmetic for anything that affects the state, as [`engine/fixed`](../engine/fixed) does.
- Change the simulation only by applying payloads. A selection, a camera, a menu, and anything else a player sees only on their own screen stay out of the simulation and its hash.
- Provide a state hash of the simulation alone. A hash that includes the local camera or selection will differ between players that are in step.
- Validate every payload you apply. It comes from another machine, and a payload can be malformed or name things its sender does not own.

## Put a scene on lockstep

Create a session with the same configuration on every machine, apart from `Local`, and a link that reaches the other players:

```go
s, err := lockstep.New(lockstep.Config{
	Game:         "mygame",
	Content:      contentHash, // identifies the level and rules, such as replay.ContentHash
	Players:      2,
	Local:        myIndex,
	Delay:        6,  // ticks between submitting and applying, see below
	HashInterval: 60, // compare state hashes once a second at 60 ticks a second
	StallLimit:   600, // end the match after ten seconds without a peer's payload
}, link)
```

Then drive it once per update, from your scene's `Update`:

```go
// step is called by the scene's Update with the tick's input.
func (g *Game) step(input engine.Input) error {
	if err := g.session.Poll(); err != nil {
		return g.end(err) // see "When a match ends"
	}
	// Turn local input into payload bytes; local-only state changes now.
	if cmd, ok := g.commandFrom(input); ok {
		if err := g.session.Submit(cmd); err != nil {
			g.notice("Too many commands this tick.")
		}
	}
	payloads, ok := g.session.Ready()
	if !ok {
		g.waiting = true // show that the match waits for another player
		return nil
	}
	g.waiting = false
	for player, payload := range payloads {
		g.apply(player, payload) // decode, validate, and apply in player order
	}
	if err := g.sim.Step(); err != nil {
		return err
	}
	return g.session.Done(g.sim.Hash())
}
```

- `Poll` reads every message that has arrived. It never blocks.
- `Submit` adds bytes to your payload for the next frame. A payload holds at most `lockstep.MaxPayload`, 1,024, bytes per tick. Several commands in one tick share it, so encode them back to back.
- `Ready` returns every player's payload for the current tick, or false while one is missing. While it returns false, do not step the simulation or the match clock.
- `Done` takes the state hash after the step, advances the tick, sends your payload, and at checkpoints compares hashes.

The match starts when every player's `Hello` has arrived and agreed; `Running` reports it. The first `Delay` ticks are empty for everyone, so the match starts without waiting for the network.

## Choose the delay

A payload submitted during tick `t` is applied at tick `t + Delay + 1` on every machine. A larger delay hides more network latency, because the payload has more time to arrive before it is needed, but every command feels later. At 60 ticks a second, a delay of 6 is 100 ms plus one tick. If the round trip between two players is longer than the delay, the match pauses on most ticks.

## When a match ends

The first error ends the session; `Poll` and `Done` keep returning it. Show the reason and leave the match:

- `*lockstep.DesyncError` means two copies of the simulation reached different states. It names the tick and both hashes. It is a bug in the game's determinism, and every affected player reports the same tick.
- `*lockstep.ProtocolError` means another player sent something malformed or out of order.
- `lockstep.ErrStalled` means another player's payloads stopped arriving for `StallLimit` updates.
- `lockstep.ErrPeerLeft` means another player called `Leave`.
- Any other error comes from the link, for example a lost connection.

Use `errors.Is` and `errors.AsType` to tell them apart. Call `Leave` when the local player quits, so the others learn it at once rather than through a stall.

## Test without a network

`lockstep.NewMemoryNet(players)` connects sessions in memory, so a test can run every player's scene in one process. It can delay messages, deliver them in bursts, and cut a player off:

```go
net := lockstep.NewMemoryNet(2)
net.SetLatency(3)
host, _ := lockstep.New(cfg0, net.Link(0))
guest, _ := lockstep.New(cfg1, net.Link(1))
// Update both scenes in turn and compare their simulation hashes.
```

## Connect through a relay

Players do not connect to each other directly, because a browser cannot listen for connections. Both connect out to a relay, which pairs them by a room code and forwards each player's messages to the other without reading them. The relay knows nothing about your game.

Run a relay with `go run ./cmd/relay -addr :8080`. It has limits that you can change with flags: `-max-rooms`, `-idle`, `-join-window`, `-rate`, `-max-message`, and `-origin` to accept browsers only from the pages you serve. It speaks plain `ws://`, so put a TLS-terminating proxy in front of it to serve `wss://`, which a page loaded over HTTPS requires. The [relay specification](../specs/relay-m16.md) lists the routes, limits, and the wire format.

On each machine, [`netlink.Dial`](../netlink/netlink.go) connects to the relay and returns a connection that is a `lockstep.Link`. It works the same way natively and in the browser, where it uses the page's own `WebSocket`:

```go
// The host opens a room and shares conn.Code() with the guest out of band.
conn, err := netlink.Dial(ctx, "ws://relay.example:8080/new")
// The guest joins it.
conn, err = netlink.Dial(ctx, "ws://relay.example:8080/join?room="+code)

// The host must wait for the guest before it creates the session.
for !conn.PeerJoined() {
	// Run a lobby scene, and give up when conn.Err() is not nil.
}
session, err := lockstep.New(cfg, conn)
```

`Dial` returns after the relay's welcome, so `conn.Slot()` is the `Local` of your configuration: 0 for the host and 1 for the guest. A host that sends before the guest has joined loses those messages, which is why it waits. Close the connection when the match ends; the other player is told that you left. Treat the relay as untrusted, as you treat the other player: it sees every message, and a malicious relay can delay or drop them, which ends the match with a stall, but it cannot make two honest copies agree on a wrong state, because they compare hashes.

The [outpost](playground.md#play-the-outpost-online) is a complete example of a lobby, a match, and the screens for how a match ends. Its `outpost_online.go` is the part that is not specific to the outpost's rules.

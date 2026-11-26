//go:build !js

package main

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/cmd/relay/relay"
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/netlink"
)

func TestLiveViewReceivesFinalTick(t *testing.T) {
	t.Parallel()
	relayer := relay.New(relay.Config{Rate: 2000})
	server := httptest.NewServer(relayer)
	t.Cleanup(server.Close)
	t.Cleanup(relayer.Close)
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	dial := func(path string) *netlink.Conn {
		t.Helper()
		conn, err := netlink.Dial(t.Context(), base+path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	host := dial("/v2/new")
	guest := dial("/v2/join?room=" + host.Code())
	observer := dial("/v2/watch?room=" + host.ViewCode())
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	until := func(ready func() bool) {
		t.Helper()
		for !ready() {
			select {
			case <-deadline.C:
				t.Fatal("the room did not complete the duel and live view")
			case <-ticker.C:
			}
		}
	}
	until(func() bool { return host.Epoch() == 1 && guest.Epoch() == 1 && host.Watcher() != 0 })
	newApp := func() *outpostApp {
		t.Helper()
		world := newOutpostWorld(t)
		var err error
		world.versusBoard, world.versusMeta, err = loadOutpostLevel(
			duelFiles(), "levels/versus.json", versusContent,
		)
		if err != nil {
			t.Fatal(err)
		}
		app := newOutpostApp(world)
		if err = app.Enter(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(app.Exit)
		return app
	}
	var apps [2]*outpostApp
	var players [2]*outpostSession
	for i, conn := range []*netlink.Conn{host, guest} {
		a := newApp()
		a.lobby = &onlineLobby{conn: conn, setup: versusSetup, cancel: func() {}}
		a.beginOnline()
		apps[i], players[i] = a, a.session
	}
	view := newApp()
	view.state, view.conn, view.live = stateLiveWatch, observer, &liveWatch{}
	press, release := drag(0, 0, width-1, height-1, 0)
	frame := 0
	until(func() bool {
		for i, app := range apps {
			if app.state != stateOnline {
				continue
			}
			var in engine.Input
			switch {
			case frame == 0:
				in = press
			case frame == 1:
				in = release
			case i == 0 && frame%30 == 2:
				in.Pressed = engine.KeyF
			case i == 1 && frame == 2:
				in.Pressed = engine.KeyH
			}
			updateApp(t, app, in)
		}
		frame++
		updateApp(t, view, engine.Input{})
		if view.state != stateLiveWatch {
			t.Fatalf("live view failed: %v", view.endLines)
		}
		drained := view.live.inbox.count == 0 && (view.live.play == nil || view.live.play.done())
		ended := apps[0].state == stateOnlineEnded && apps[1].state == stateOnlineEnded
		return ended && view.live.recvErr != nil && drained
	})
	if players[0].outcome != outcomeVictory || players[1].outcome != outcomeDefeat {
		t.Fatalf("unexpected duel result: %v / %v", apps[0].endLines, apps[1].endLines)
	}
	for i, a := range apps {
		if len(a.endLines) != 1 || !strings.HasPrefix(a.endLines[0], "Match complete:") {
			t.Fatalf("player %d did not confirm the natural result: %v", i, a.endLines)
		}
	}
	if view.session == nil || view.session.outcome != outcomeVictory ||
		view.session.sim.Ticks() != players[0].sim.Ticks() || view.session.sim.Trace() != players[0].sim.Trace() {
		t.Fatalf(
			"live view ended at tick %d; want its verified result at %d",
			view.live.applied,
			players[0].sim.Ticks(),
		)
	}
	if !bytes.Equal(apps[0].lastMatch, apps[1].lastMatch) {
		t.Fatal("players kept different histories for the same result")
	}
}

//go:build !(js && wasm)

package netlink

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/krapht-foundation/goje/netlink/ws"
)

// writeTimeout bounds each write to the relay. It matches the lockstep
// session's ten-second peer timeout: a relay that accepts nothing for that
// long is gone.
const writeTimeout = 10 * time.Second

// Dial connects to a relay URL, such as ws://host:8080/new to open a room or
// ws://host:8080/join?room=CODE to join one, and returns once the relay's
// Welcome has arrived. The v2 routes /v2/new, /v2/join?room=CODE, and
// /v2/watch?room=VIEW work the same way. It blocks, so a game calls it from a
// goroutine. The context bounds connecting and waiting for the Welcome only.
func Dial(ctx context.Context, rawURL string) (*Conn, error) {
	return dial(ctx, rawURL, nil)
}

// Resume takes a v2 player's slot back after ErrLost. rawURL is the relay's
// /v2/resume route, such as wss://host/v2/resume, and token the lost
// connection's ResumeToken, which is sent as the first message, never in the
// URL. It fails with ErrPeerLeft when the room has ended or the token is
// unknown, and with an error wrapping ErrLost when the relay still sees the
// old connection or the network failed, in which case trying again within
// the 30-second hold may succeed.
func Resume(ctx context.Context, rawURL string, token [32]byte) (*Conn, error) {
	return dial(ctx, rawURL, resumeMessage(token))
}

func dial(ctx context.Context, rawURL string, first []byte) (*Conn, error) {
	// The relay prefixes each forwarded message with a header.
	wc, err := ws.Dial(ctx, rawURL, ws.Options{Limit: maxFrame, WriteTimeout: writeTimeout})
	if err != nil {
		return nil, fmt.Errorf("netlink: %w", err)
	}
	buf := make([]byte, maxFrame)
	// Closing the connection is the only way to interrupt a read, and a
	// cancelled Dial discards the connection anyway.
	stop := context.AfterFunc(ctx, func() {
		//nolint:errcheck // The read below reports the failure.
		_ = wc.Close()
	})
	if first != nil {
		err = wc.WriteMessage(first)
	}
	var msg []byte
	if err == nil {
		msg, err = wc.ReadMessage(buf)
	}
	if !stop() {
		err = errors.Join(ctx.Err(), err)
	}
	var w welcome
	switch {
	case err != nil && first != nil:
		err = lost(err)
	case err == nil:
		w, err = parseFirst(msg, first != nil)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("netlink: welcome: %w", err), wc.Close())
	}
	c := newConn(wsTransport{wc}, w)
	go c.writeLoop()
	go readLoop(c, wc, buf)
	return c, nil
}

// readLoop feeds the relay's messages to c until the connection fails.
func readLoop(c *Conn, wc *ws.Conn, buf []byte) {
	for {
		msg, err := wc.ReadMessage(buf)
		if err != nil {
			c.fail(lost(err))
			return
		}
		if err := c.receive(msg); err != nil {
			c.fail(err)
			return
		}
	}
}

type wsTransport struct{ c *ws.Conn }

func (t wsTransport) write(msg []byte) error { return t.c.WriteMessage(msg) }

func (t wsTransport) close(lost bool) {
	code := uint16(1000)
	if lost {
		code = closeLost
	}
	//nolint:errcheck // The connection is finished; there is nothing to recover.
	_ = t.c.CloseWith(code)
}

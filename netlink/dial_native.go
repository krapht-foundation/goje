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
// Welcome has arrived. It blocks, so a game calls it from a goroutine. The
// context bounds connecting and waiting for the Welcome only.
func Dial(ctx context.Context, rawURL string) (*Conn, error) {
	// The relay prefixes each forwarded message with one type byte.
	wc, err := ws.Dial(ctx, rawURL, ws.Options{Limit: MaxMessage + 1, WriteTimeout: writeTimeout})
	if err != nil {
		return nil, fmt.Errorf("netlink: %w", err)
	}
	buf := make([]byte, MaxMessage+1)
	// Closing the connection is the only way to interrupt a read, and a
	// cancelled Dial discards the connection anyway.
	stop := context.AfterFunc(ctx, func() {
		//nolint:errcheck // The read below reports the failure.
		_ = wc.Close()
	})
	msg, err := wc.ReadMessage(buf)
	if !stop() {
		err = errors.Join(ctx.Err(), err)
	}
	var slot int
	var code string
	if err == nil {
		slot, code, err = parseWelcome(msg)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("netlink: welcome: %w", err), wc.Close())
	}
	c := newConn(wsTransport{wc}, slot, code)
	go c.writeLoop()
	go readLoop(c, wc, buf)
	return c, nil
}

// readLoop feeds the relay's messages to c until the connection fails.
func readLoop(c *Conn, wc *ws.Conn, buf []byte) {
	for {
		msg, err := wc.ReadMessage(buf)
		if err == nil {
			err = c.receive(msg)
		}
		if err != nil {
			c.fail(fmt.Errorf("netlink: %w", err))
			return
		}
	}
}

type wsTransport struct{ c *ws.Conn }

func (t wsTransport) write(msg []byte) error { return t.c.WriteMessage(msg) }

func (t wsTransport) close() {
	//nolint:errcheck // The connection is finished; there is nothing to recover.
	_ = t.c.Close()
}

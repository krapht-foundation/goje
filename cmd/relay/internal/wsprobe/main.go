// Command wsprobe is the native side of scripts/smoke-ws.mjs. It joins a relay
// room with netlink.Dial, sends numbered messages, checks the ones it receives
// from the browser, closes, and prints a one-line JSON summary. The message
// pattern must match the script's.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/krapht-foundation/goje/netlink"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wsprobe:", err)
		os.Exit(1)
	}
}

// message returns the i-th of n messages sent by the player in slot. The last
// is the largest a client may send.
func message(slot, i, n int) []byte {
	size := 1 + i*37%netlink.MaxMessage
	if i == n-1 {
		size = netlink.MaxMessage
	}
	p := make([]byte, size)
	for j := range p {
		p[j] = byte(i*7 + j + slot*13) // #nosec G115 -- the pattern wraps by design.
	}
	return p
}

func run() error {
	url := flag.String("url", "", "relay URL to dial, such as ws://127.0.0.1:8080/join?room=CODE")
	n := flag.Int("n", 200, "messages to send and to expect")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := netlink.Dial(ctx, *url)
	if err != nil {
		return err
	}
	// Close is not needed: the process exits once the peer has left.
	for i := 0; i < *n; {
		switch sendErr := c.Send(message(c.Slot(), i, *n)); {
		case sendErr == nil:
			i++
		case errors.Is(sendErr, netlink.ErrQueueFull):
			time.Sleep(time.Millisecond)
		default:
			return fmt.Errorf("send %d: %w", i, sendErr)
		}
	}
	received := 0
	for received < *n {
		if ctx.Err() != nil {
			return fmt.Errorf("received %d of %d messages: %w", received, *n, ctx.Err())
		}
		msg, ok, recvErr := c.Recv()
		if recvErr != nil {
			return fmt.Errorf("receive %d: %w", received, recvErr)
		}
		if !ok {
			time.Sleep(time.Millisecond)
			continue
		}
		if want := message(1-c.Slot(), received, *n); !bytes.Equal(msg, want) {
			return fmt.Errorf("message %d: %d bytes, want %d", received, len(msg), len(want))
		}
		received++
	}
	// The browser leaves once it has every message, which also shows that this
	// side's queue was written before the process exits.
	for {
		_, ok, recvErr := c.Recv()
		if errors.Is(recvErr, netlink.ErrPeerLeft) {
			break
		}
		if ok || recvErr != nil || ctx.Err() != nil {
			return fmt.Errorf("waiting for the peer to leave: %v, %w, %w", ok, recvErr, ctx.Err())
		}
		time.Sleep(time.Millisecond)
	}
	// The room code comes from the relay's alphabet, so %q is valid JSON.
	fmt.Printf("{\"slot\":%d,\"code\":%q,\"sent\":%d,\"received\":%d}\n", c.Slot(), c.Code(), *n,
		received)
	return nil
}

//go:build js && wasm

package netlink

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"syscall/js"
	"time"
)

// maxBuffered bounds the bytes the browser may hold unsent. Above it the
// writer waits, as a native write blocks on a full socket, so the send queue
// fills and Send reports ErrQueueFull instead of the page buffering without
// limit.
const maxBuffered = sendQueue * maxFrame

// Dial connects to a relay URL, such as wss://host/new to open a room or
// wss://host/join?room=CODE to join one, through the page's WebSocket and
// returns once the relay's Welcome has arrived. The v2 routes /v2/new,
// /v2/join?room=CODE, and /v2/watch?room=VIEW work the same way. It blocks,
// so a game calls it from a goroutine, never from a JavaScript callback. The
// context bounds waiting for the Welcome only.
func Dial(ctx context.Context, rawURL string) (*Conn, error) {
	return dial(ctx, rawURL, nil)
}

// Resume takes a v2 player's slot back after ErrLost. rawURL is the relay's
// /v2/resume route, such as wss://host/v2/resume, and token the lost
// connection's ResumeToken, which is sent as the first message once the
// socket opens, never in the URL. It fails with ErrPeerLeft when the room has
// ended or the token is unknown, and with an error wrapping ErrLost when the
// relay still sees the old connection or the network failed, in which case
// trying again within the 30-second hold may succeed.
func Resume(ctx context.Context, rawURL string, token [32]byte) (*Conn, error) {
	return dial(ctx, rawURL, resumeMessage(token))
}

func dial(ctx context.Context, rawURL string, first []byte) (*Conn, error) {
	// Checking the scheme first avoids the constructor's SyntaxError.
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		return nil, fmt.Errorf("netlink: %q is not a ws or wss URL", rawURL)
	}
	ctor := js.Global().Get("WebSocket")
	if !ctor.Truthy() {
		return nil, errors.New("netlink: this browser has no WebSocket")
	}
	t := &jsTransport{
		arrayBuffer: js.Global().Get("ArrayBuffer"),
		uint8Array:  js.Global().Get("Uint8Array"),
		welcome:     make(chan error, 1),
		first:       first,
	}
	if err := catch(func() { t.sock = ctor.New(rawURL) }); err != nil {
		return nil, fmt.Errorf("netlink: %w", err)
	}
	t.sock.Set("binaryType", "arraybuffer")
	t.wview = t.uint8Array.New(maxFrame)
	t.onMessage = js.FuncOf(t.message)
	t.onClose = js.FuncOf(t.closed)
	t.onOpen = js.FuncOf(t.opened)
	t.sock.Set("onmessage", t.onMessage)
	t.sock.Set("onclose", t.onClose)
	t.sock.Set("onopen", t.onOpen)
	var err error
	select {
	case err = <-t.welcome:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		t.close(false)
		return nil, fmt.Errorf("netlink: welcome: %w", err)
	}
	go t.conn.writeLoop()
	return t.conn, nil
}

// jsTransport is a browser WebSocket. Its callbacks run on the page's event
// loop and only copy bytes into the Conn's queues.
type jsTransport struct {
	sock, arrayBuffer, uint8Array js.Value
	// wview is a reused Uint8Array that outgoing messages are copied into.
	wview                      js.Value
	onMessage, onClose, onOpen js.Func
	// rbuf receives each incoming message before it is queued.
	rbuf [maxFrame]byte
	// first is the resume message, sent when the socket opens; nil for Dial.
	first []byte
	// conn is set when the Welcome arrives; until then failures go to welcome.
	conn      *Conn
	welcome   chan error
	closeOnce sync.Once
}

func (t *jsTransport) opened(js.Value, []js.Value) any {
	if t.first != nil {
		if err := t.write(t.first); err != nil {
			t.failed(lost(err))
		}
	}
	return nil
}

func (t *jsTransport) message(_ js.Value, args []js.Value) any {
	data := args[0].Get("data")
	var err error
	if !data.InstanceOf(t.arrayBuffer) {
		err = fmt.Errorf("%w: text message", ErrProtocol)
	} else if n := data.Get("byteLength").Int(); n > len(t.rbuf) {
		err = fmt.Errorf("%w: message of %d bytes", ErrProtocol, n)
	} else {
		msg := t.rbuf[:n]
		js.CopyBytesToGo(msg, t.uint8Array.New(data))
		err = t.handle(msg)
	}
	if err != nil {
		t.failed(err)
	}
	return nil
}

func (t *jsTransport) handle(msg []byte) error {
	if t.conn != nil {
		return t.conn.receive(msg)
	}
	w, err := parseFirst(msg, t.first != nil)
	if err != nil {
		return err
	}
	t.conn = newConn(t, w)
	// Non-blocking: an earlier failure may already fill the channel, and Dial
	// then discards this connection.
	select {
	case t.welcome <- nil:
	default:
	}
	return nil
}

func (t *jsTransport) closed(_ js.Value, args []js.Value) any {
	ev := args[0]
	t.failed(fmt.Errorf("%w: closed by the relay (status %d)", ErrLost, ev.Get("code").Int()))
	return nil
}

// failed ends the connection: before the Welcome it fails Dial, and after it
// fails the Conn, whose writer then closes the socket.
func (t *jsTransport) failed(err error) {
	if t.conn != nil {
		t.conn.fail(err)
		return
	}
	select {
	case t.welcome <- err:
	default:
	}
}

func (t *jsTransport) write(msg []byte) error {
	// The writer runs on its own goroutine, so waiting here lets the event
	// loop send what is buffered. The resume message is written from onopen
	// to an empty buffer and never waits.
	for t.sock.Get("bufferedAmount").Int() > maxBuffered {
		time.Sleep(10 * time.Millisecond)
	}
	js.CopyBytesToJS(t.wview, msg)
	return catch(func() { t.sock.Call("send", t.wview.Call("subarray", 0, len(msg))) })
}

// close closes the socket and releases the callbacks. The handlers are
// removed first, so the browser cannot call a released function.
func (t *jsTransport) close(lost bool) {
	t.closeOnce.Do(func() {
		t.sock.Set("onmessage", js.Null())
		t.sock.Set("onclose", js.Null())
		t.sock.Set("onopen", js.Null())
		code := 1000
		if lost {
			code = closeLost
		}
		//nolint:errcheck // Closing a socket that is already closing is harmless.
		_ = catch(func() { t.sock.Call("close", code) })
		t.onMessage.Release()
		t.onClose.Release()
		t.onOpen.Release()
	})
}

// catch turns a JavaScript exception thrown by f into an error.
func catch(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("netlink: browser exception: %v", r)
		}
	}()
	f()
	return nil
}

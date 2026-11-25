package ws

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6455 requires SHA-1 for Sec-WebSocket-Accept; it is not used for security.
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// acceptGUID is the fixed string of RFC 6455 section 1.3.
const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxResponse bounds the bytes a client reads while it waits for the server's
// handshake response, so a hostile server cannot make it buffer without end.
const maxResponse = 16 << 10

// AcceptKey returns the Sec-WebSocket-Accept value for a Sec-WebSocket-Key.
func AcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + acceptGUID)) // #nosec G401 -- required by RFC 6455.
	return base64.StdEncoding.EncodeToString(sum[:])
}

// handshakeError is a rejected upgrade request and the HTTP status to answer.
type handshakeError struct {
	status int
	reason string
}

func (e *handshakeError) Error() string { return "ws: bad handshake: " + e.reason }

func (e *handshakeError) Unwrap() error { return ErrProtocol }

// checkRequest validates an upgrade request and returns its key. The server's
// http.Server bounds the request's size with MaxHeaderBytes.
func checkRequest(r *http.Request, origins []string) (string, error) {
	switch {
	case r.Method != http.MethodGet:
		return "", &handshakeError{http.StatusMethodNotAllowed, "method is not GET"}
	case !r.ProtoAtLeast(1, 1):
		return "", &handshakeError{http.StatusBadRequest, "protocol older than HTTP/1.1"}
	case !hasToken(r.Header, "Upgrade", "websocket"):
		return "", &handshakeError{http.StatusBadRequest, "missing Upgrade: websocket"}
	case !hasToken(r.Header, "Connection", "upgrade"):
		return "", &handshakeError{http.StatusBadRequest, "missing Connection: upgrade"}
	case r.Header.Get("Sec-WebSocket-Version") != "13":
		return "", &handshakeError{http.StatusUpgradeRequired, "unsupported version"}
	}
	keys := r.Header.Values("Sec-WebSocket-Key")
	if len(keys) != 1 {
		return "", &handshakeError{http.StatusBadRequest, "missing or repeated key"}
	}
	if raw, err := base64.StdEncoding.DecodeString(keys[0]); err != nil || len(raw) != 16 {
		return "", &handshakeError{http.StatusBadRequest, "key is not 16 bytes in base64"}
	}
	origin := r.Header.Get("Origin")
	if origin != "" && len(origins) > 0 && !slices.Contains(origins, origin) {
		return "", &handshakeError{http.StatusForbidden, "origin not allowed"}
	}
	return keys[0], nil
}

// hasToken reports whether a comma-separated header contains token, ignoring
// case.
func hasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for part := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// Accept upgrades an HTTP request to a server-side WebSocket connection. It
// offers no extension or subprotocol. When the request is not a valid upgrade
// it answers with an HTTP error status and returns an error wrapping
// ErrProtocol; after success the handler must not use w again.
func Accept(w http.ResponseWriter, r *http.Request, opts Options) (*Conn, error) {
	key, err := checkRequest(r, opts.Origins)
	if he, ok := errors.AsType[*handshakeError](err); ok {
		if he.status == http.StatusUpgradeRequired {
			w.Header().Set("Sec-WebSocket-Version", "13")
		}
		http.Error(w, http.StatusText(he.status), he.status)
		return nil, err
	}
	nc, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return nil, fmt.Errorf("ws: hijack: %w", err)
	}
	// The server may have left its own deadlines on the hijacked connection;
	// from here on every read and write sets its own.
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Accept: " + AcceptKey(key) + "\r\n\r\n"
	err = nc.SetDeadline(time.Time{})
	if err == nil {
		err = nc.SetWriteDeadline(time.Now().Add(opts.writeTimeout()))
	}
	if err == nil {
		_, err = io.WriteString(nc, resp)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("ws: write handshake: %w", err), nc.Close())
	}
	// The hijacked reader may already hold frames the client sent early.
	return newConn(nc, rw.Reader, false, opts), nil
}

// Dial opens a client connection to a ws:// or wss:// URL. The context bounds
// connecting and the handshake, which are also bounded by the write timeout;
// it does not affect the returned connection. Dial rejects a response that is
// not 101, has a wrong accept value, or selects an extension or subprotocol.
func Dial(ctx context.Context, rawURL string, opts Options) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ws: parse URL: %w", err)
	}
	port := "80"
	switch u.Scheme {
	case "ws":
	case "wss":
		port = "443"
	default:
		return nil, fmt.Errorf("ws: URL scheme %q is not ws or wss", u.Scheme)
	}
	if u.Port() != "" {
		port = u.Port()
	}
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, fmt.Errorf("ws: dial: %w", err)
	}
	// Cancelling the context interrupts the handshake by expiring the deadline.
	stop := context.AfterFunc(ctx, func() {
		//nolint:errcheck // Failing to expire the deadline leaves only the write timeout.
		_ = nc.SetDeadline(time.Unix(1, 0))
	})
	c, err := handshake(ctx, nc, u, opts)
	if !stop() {
		err = errors.Join(ctx.Err(), err)
	}
	if err == nil {
		err = nc.SetDeadline(time.Time{})
	}
	if err != nil {
		return nil, errors.Join(err, nc.Close())
	}
	return c, nil
}

// handshake runs the client side of the opening handshake on nc, wrapping it
// in TLS first for wss.
func handshake(ctx context.Context, nc net.Conn, u *url.URL, opts Options) (*Conn, error) {
	if err := nc.SetDeadline(time.Now().Add(opts.writeTimeout())); err != nil {
		return nil, fmt.Errorf("ws: set handshake deadline: %w", err)
	}
	if u.Scheme == "wss" {
		tc := tls.Client(nc, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tc.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("ws: TLS handshake: %w", err)
		}
		nc = tc
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("ws: handshake key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(raw[:])
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host +
		"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: " + key +
		"\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(nc, req); err != nil {
		return nil, fmt.Errorf("ws: write handshake: %w", err)
	}
	budget := &budgetReader{r: nc, n: maxResponse}
	br := bufio.NewReader(budget)
	if err := checkResponse(br, key); err != nil {
		return nil, err
	}
	// Frames are bounded by the frame header checks from here on.
	budget.n = -1
	return newConn(nc, br, true, opts), nil
}

// checkResponse reads the server's handshake response and validates it.
func checkResponse(br *bufio.Reader, key string) error {
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return fmt.Errorf("ws: read handshake response: %w", err)
	}
	// A 101 response has no body; any other status is rejected unread.
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("ws: handshake response: %w", err)
	}
	switch {
	case resp.StatusCode != http.StatusSwitchingProtocols:
		return fmt.Errorf("%w: status %s", ErrProtocol, resp.Status)
	case !hasToken(resp.Header, "Upgrade", "websocket"):
		return fmt.Errorf("%w: response lacks Upgrade: websocket", ErrProtocol)
	case !hasToken(resp.Header, "Connection", "upgrade"):
		return fmt.Errorf("%w: response lacks Connection: upgrade", ErrProtocol)
	case resp.Header.Get("Sec-WebSocket-Accept") != AcceptKey(key):
		return fmt.Errorf("%w: wrong Sec-WebSocket-Accept", ErrProtocol)
	case len(resp.Header.Values("Sec-WebSocket-Extensions")) > 0:
		return fmt.Errorf("%w: server selected an extension", ErrProtocol)
	case len(resp.Header.Values("Sec-WebSocket-Protocol")) > 0:
		return fmt.Errorf("%w: server selected a subprotocol", ErrProtocol)
	}
	return nil
}

// budgetReader fails once it has read n bytes; a negative n is unlimited.
type budgetReader struct {
	r io.Reader
	n int
}

var errResponseTooLarge = fmt.Errorf("%w: handshake response too large", ErrProtocol)

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.n < 0 {
		return b.r.Read(p) //nolint:wrapcheck // A transparent reader; callers wrap.
	}
	if b.n == 0 {
		return 0, errResponseTooLarge
	}
	if len(p) > b.n {
		p = p[:b.n]
	}
	n, err := b.r.Read(p)
	b.n -= n
	return n, err //nolint:wrapcheck // A transparent reader; callers wrap.
}

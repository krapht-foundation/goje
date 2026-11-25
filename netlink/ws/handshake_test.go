package ws

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleKey = "dGhlIHNhbXBsZSBub25jZQ=="

func upgradeRequest() *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/new", http.NoBody)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", sampleKey)
	return r
}

func TestCheckRequest(t *testing.T) {
	origins := []string{"https://play.example"}
	cases := []struct {
		name   string
		edit   func(r *http.Request)
		status int // Zero means accepted.
	}{
		{"valid", func(*http.Request) {}, 0},
		{"case-insensitive tokens", func(r *http.Request) {
			r.Header.Set("Upgrade", "WebSocket")
			r.Header.Set("Connection", "UPGRADE")
		}, 0},
		{"allowed origin", func(r *http.Request) { r.Header.Set("Origin", origins[0]) }, 0},
		{"no origin", func(r *http.Request) { r.Header.Del("Origin") }, 0},
		{"POST", func(r *http.Request) { r.Method = http.MethodPost }, http.StatusMethodNotAllowed},
		{"HTTP/1.0", func(r *http.Request) { r.ProtoMinor = 0 }, http.StatusBadRequest},
		{"no Upgrade", func(r *http.Request) { r.Header.Del("Upgrade") }, http.StatusBadRequest},
		{
			"other Upgrade", func(r *http.Request) { r.Header.Set("Upgrade", "h2c") },
			http.StatusBadRequest,
		},
		{
			"no Connection", func(r *http.Request) { r.Header.Del("Connection") },
			http.StatusBadRequest,
		},
		{"Connection without upgrade", func(r *http.Request) {
			r.Header.Set("Connection", "keep-alive")
		}, http.StatusBadRequest},
		{
			"no version", func(r *http.Request) { r.Header.Del("Sec-WebSocket-Version") },
			http.StatusUpgradeRequired,
		},
		{
			"version 8", func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "8") },
			http.StatusUpgradeRequired,
		},
		{
			"no key", func(r *http.Request) { r.Header.Del("Sec-WebSocket-Key") },
			http.StatusBadRequest,
		},
		{
			"two keys", func(r *http.Request) { r.Header.Add("Sec-WebSocket-Key", sampleKey) },
			http.StatusBadRequest,
		},
		{
			"key not base64", func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "!!!!") },
			http.StatusBadRequest,
		},
		{"key of 15 bytes", func(r *http.Request) {
			r.Header.Set("Sec-WebSocket-Key", "AAAAAAAAAAAAAAAAAAAA")
		}, http.StatusBadRequest},
		{"origin not allowed", func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.example")
		}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := upgradeRequest()
			tc.edit(r)
			key, err := checkRequest(r, origins)
			if tc.status == 0 {
				if err != nil || key != sampleKey {
					t.Fatalf("checkRequest = %q, %v", key, err)
				}
				return
			}
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("checkRequest error %v", err)
			}
			w := httptest.NewRecorder()
			if c, err := Accept(w, r, Options{Origins: origins}); c != nil || err == nil {
				t.Fatal("Accept upgraded a bad request")
			}
			if w.Code != tc.status {
				t.Errorf("status %d, want %d", w.Code, tc.status)
			}
		})
	}
}

func TestAcceptNeedsHijacker(t *testing.T) {
	w := httptest.NewRecorder()
	if _, err := Accept(w, upgradeRequest(), Options{}); err == nil ||
		w.Code != http.StatusInternalServerError {
		t.Fatalf("Accept without a hijacker = %v, status %d", err, w.Code)
	}
}

func response(edit func(h http.Header)) string {
	h := http.Header{}
	h.Set("Upgrade", "websocket")
	h.Set("Connection", "Upgrade")
	h.Set("Sec-WebSocket-Accept", AcceptKey(sampleKey))
	edit(h)
	var b strings.Builder
	b.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	if err := h.Write(&b); err != nil {
		panic(err)
	}
	b.WriteString("\r\n")
	return b.String()
}

func readResponse(s string) error {
	return checkResponse(bufio.NewReader(&budgetReader{r: strings.NewReader(s), n: maxResponse}),
		sampleKey)
}

func TestCheckResponse(t *testing.T) {
	if err := readResponse(response(func(http.Header) {})); err != nil {
		t.Fatalf("valid response: %v", err)
	}
	bad := map[string]string{
		"status 200": strings.Replace(
			response(func(http.Header) {}),
			"101 Switching Protocols",
			"200 OK",
			1,
		),
		"no Upgrade":    response(func(h http.Header) { h.Del("Upgrade") }),
		"no Connection": response(func(h http.Header) { h.Del("Connection") }),
		"wrong accept": response(func(h http.Header) {
			h.Set("Sec-WebSocket-Accept", AcceptKey("other"))
		}),
		"extension": response(func(h http.Header) {
			h.Set("Sec-WebSocket-Extensions", "permessage-deflate")
		}),
		"subprotocol": response(func(h http.Header) { h.Set("Sec-WebSocket-Protocol", "chat") }),
		"oversized": response(func(h http.Header) {
			h.Set("X-Padding", strings.Repeat("a", maxResponse))
		}),
		"garbage": "not http\r\n\r\n",
	}
	for name, resp := range bad {
		if err := readResponse(resp); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func FuzzHandshake(f *testing.F) {
	req := "GET /new HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + sampleKey + "\r\n\r\n"
	f.Add([]byte(req))
	f.Add([]byte(response(func(http.Header) {})))
	f.Add([]byte("HTTP/1.1 101 OK\r\nSec-WebSocket-Extensions: x\r\n\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// The same bytes as a request to the server and as a response to the
		// client.
		if r, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data))); err == nil {
			key, err := checkRequest(r, []string{"https://a.example"})
			if err == nil && key == "" {
				t.Fatal("accepted a request without a key")
			}
		}
		budget := &budgetReader{r: bytes.NewReader(data), n: maxResponse}
		if err := checkResponse(bufio.NewReader(budget), sampleKey); err == nil &&
			!strings.Contains(string(data), AcceptKey(sampleKey)) {
			t.Fatal("accepted a response without the accept value")
		}
	})
}

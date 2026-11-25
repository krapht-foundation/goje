package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWebPage(t *testing.T) {
	t.Parallel()
	page, err := webPage("beacon")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	for _, want := range []string{
		`<title>beacon</title>`,
		`<canvas id="goje" tabindex="0" aria-label="beacon">`,
		`content="width=device-width, initial-scale=1, viewport-fit=cover, user-scalable=no"`,
		`<link rel="manifest" href="manifest.webmanifest">`,
		`touch-action: none`, `env(safe-area-inset-left)`,
		`<script src="wasm_exec.js"></script>`, `fetch("game.wasm")`,
		`does not support WebAssembly`, `does not support WebGL 2`, `failed to download`,
		`navigator.serviceWorker.register("sw.js")`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if n := strings.Count(text, "new Go()"); n != 1 {
		t.Errorf("page starts the Go runtime %d times, want once", n)
	}
	if n := strings.Count(text, "<canvas"); n != 1 {
		t.Errorf("page has %d canvases, want one", n)
	}
	inline := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(text, -1)
	if len(inline) != 1 || len(inline[0][1]) >= 4096 {
		t.Errorf("page has %d inline scripts; want one under 4 KiB", len(inline))
	}
	if !regexp.MustCompile(`(?s)main \{[^}]*touch-action: none;[^}]*safe-area-inset-top`).
		MatchString(text) {
		t.Error("the canvas container lacks touch-action or safe-area padding")
	}
}

func TestWebManifest(t *testing.T) {
	t.Parallel()
	data, err := webManifest("beacon")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name": "beacon", "short_name": "beacon", "start_url": "./", "scope": "./",
		"display": "fullscreen", "background_color": background, "theme_color": background,
		"icons": []any{
			map[string]any{
				"src": "icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any",
			},
			map[string]any{
				"src": "icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable",
			},
		},
	}
	gotText, _ := json.Marshal(got)
	wantText, _ := json.Marshal(want)
	if !bytes.Equal(gotText, wantText) {
		t.Fatalf("manifest = %s, want %s", gotText, wantText)
	}
}

func TestServiceWorker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "page")
	write("game.wasm", "wasm")
	first, err := serviceWorker(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The cache name is the digest of the bundle's SHA256SUMS lines, in name order.
	wasm, page := sha256.Sum256([]byte("wasm")), sha256.Sum256([]byte("page"))
	bundle := sha256.Sum256(fmt.Appendf(nil, "%x  game.wasm\n%x  index.html\n", wasm, page))
	for _, want := range []string{
		fmt.Sprintf("const cache = prefix + %q;", hex.EncodeToString(bundle[:])),
		"const prefix = `goje ${self.registration.scope} `;",
		`const files = ["./","game.wasm","index.html"];`,
		`key.startsWith(prefix) && key !== cache`, `caches.delete(key)`,
	} {
		if !strings.Contains(string(first), want) {
			t.Errorf("sw.js lacks %q:\n%s", want, first)
		}
	}
	again, err := serviceWorker(dir)
	if err != nil || !bytes.Equal(again, first) {
		t.Fatalf("the same bundle gave a different worker (%v)", err)
	}
	write("game.wasm", "wasm2")
	changed, err := serviceWorker(dir)
	if err != nil || bytes.Equal(changed, first) {
		t.Fatalf("a changed file kept the worker (%v)", err)
	}
}

func TestGoBuildArgs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ goos, tags, want string }{
		{"js", "", "build -trimpath -buildvcs=false -ldflags=-s -w -o OUT -- ./game"},
		{"linux", "a,b", "build -trimpath -buildvcs=false -ldflags=-s -w -o OUT -tags=a,b -- ./game"},
		{"darwin", "", "build -trimpath -buildvcs=false -ldflags=-s -w -o OUT -- ./game"},
		// A GUI program: Windows opens no console window beside the game.
		{"windows", "", "build -trimpath -buildvcs=false -ldflags=-s -w -H=windowsgui -o OUT " +
			"-- ./game"},
	} {
		got := strings.Join(goBuildArgs(test.goos, "OUT", test.tags, "./game"), " ")
		if got != test.want {
			t.Errorf("%s: args = %q, want %q", test.goos, got, test.want)
		}
	}
}

func TestInfoPlist(t *testing.T) {
	t.Parallel()
	plist := infoPlist("beacon", "1.2.3-rc.1", "org.example.beacon")
	decoder := xml.NewDecoder(bytes.NewReader(plist))
	values := map[string]string{}
	var key, text string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("plist is not valid XML: %v", err)
		}
		switch token := token.(type) {
		case xml.CharData:
			text = string(token)
		case xml.EndElement:
			switch token.Name.Local {
			case "key":
				key = text
			case "string":
				values[key] = text
			case "true":
				values[key] = "true"
			}
		}
	}
	want := map[string]string{
		"CFBundleDevelopmentRegion":     "en",
		"CFBundleName":                  "beacon",
		"CFBundleDisplayName":           "beacon",
		"CFBundleIdentifier":            "org.example.beacon",
		"CFBundleVersion":               "1.2.3",
		"CFBundleShortVersionString":    "1.2.3",
		"CFBundleExecutable":            "beacon",
		"CFBundleIconFile":              "beacon.icns",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundlePackageType":           "APPL",
		"NSHighResolutionCapable":       "true",
		"LSApplicationCategoryType":     "public.app-category.games",
		"LSMinimumSystemVersion":        "12.0",
	}
	if len(values) != len(want) {
		t.Errorf("plist has %d keys, want %d: %v", len(values), len(want), values)
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("%s = %q, want %q", k, values[k], v)
		}
	}
}

func TestIcns(t *testing.T) {
	t.Parallel()
	data := icns(defaultIcon)
	if len(data) != 16+len(defaultIcon) || string(data[:4]) != "icns" ||
		string(data[8:12]) != "ic09" ||
		binary.BigEndian.Uint32(data[4:]) != uint32(len(data)) ||
		binary.BigEndian.Uint32(data[12:]) != uint32(8+len(defaultIcon)) ||
		!bytes.Equal(data[16:], defaultIcon) {
		t.Fatalf("icns header = % x", data[:16])
	}
}

func TestCheckIcon(t *testing.T) {
	t.Parallel()
	if err := checkIcon(defaultIcon); err != nil {
		t.Fatalf("default icon: %v", err)
	}
	encode := func(tb testing.TB, w, h int) []byte {
		tb.Helper()
		var out bytes.Buffer
		if err := png.Encode(&out, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
			tb.Fatal(err)
		}
		return out.Bytes()
	}
	for name, test := range map[string]struct {
		data []byte
		want string
	}{
		"not png":    {[]byte("GIF89a"), "must be a PNG"},
		"truncated":  {defaultIcon[:len(defaultIcon)/2], "must be a PNG"},
		"small":      {encode(t, 256, 256), "not 256 × 256"},
		"not square": {encode(t, 512, 256), "not 512 × 256"},
	} {
		if err := checkIcon(test.data); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", name, err, test.want)
		}
	}
}

func TestDesktopEntry(t *testing.T) {
	t.Parallel()
	want := "[Desktop Entry]\nType=Application\nName=beacon\nExec=beacon\nIcon=beacon\n" +
		"Categories=Game;\nTerminal=false\n"
	if got := string(desktopEntry("beacon")); got != want {
		t.Fatalf("desktop entry = %q, want %q", got, want)
	}
}

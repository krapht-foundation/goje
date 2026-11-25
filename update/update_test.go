package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testKey(tb testing.TB) (ed25519.PublicKey, ed25519.PrivateKey) {
	tb.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	return public, private
}

func fileOf(name string, data []byte) File {
	sum := sha256.Sum256(data)
	return File{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

var (
	webZip   = []byte("web archive")
	linuxTar = []byte("linux archive")
)

func manifest() Manifest {
	return Manifest{Name: "beacon", Version: "1.2.0", Files: []File{
		fileOf("beacon-1.2.0-linux-amd64.tar.gz", linuxTar), fileOf("beacon-1.2.0-web.zip", webZip),
	}}
}

func TestSignVerify(t *testing.T) {
	t.Parallel()
	public, private := testKey(t)
	data, err := Sign(manifest(), private)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Sign(manifest(), private)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("signing twice gave different bytes (%v)", err)
	}
	prefix := "{\n  \"format\": \"goje-update-v1\",\n  \"name\": \"beacon\","
	if !bytes.HasPrefix(data, []byte(prefix)) ||
		!bytes.HasSuffix(data, []byte("\"\n}\n")) {
		t.Fatalf("unexpected layout:\n%s", data)
	}
	got, err := Verify(data, public)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "beacon" || got.Version != "1.2.0" || len(got.Files) != 2 ||
		got.Files[1] != manifest().Files[1] {
		t.Fatalf("verified %+v", got)
	}
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	public, private := testKey(t)
	other, _ := testKey(t)
	data, err := Sign(manifest(), private)
	if err != nil {
		t.Fatal(err)
	}
	replace := func(old, new string) []byte {
		return bytes.Replace(data, []byte(old), []byte(new), 1)
	}
	// Re-signing a changed manifest with another key must not pass either.
	forged := manifest()
	forged.Version = "9.0.0"
	_, attacker := testKey(t)
	forgedData, err := Sign(forged, attacker)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		data []byte
		key  ed25519.PublicKey
		want string
	}{
		"wrong key":       {data, other, "signature does not match"},
		"short key":       {data, public[:31], "public key has 31 bytes"},
		"changed version": {replace(`"1.2.0"`, `"1.3.0"`), public, "signature does not match"},
		"changed name":    {replace(`"beacon"`, `"beacom"`), public, "signature does not match"},
		"changed size":    {replace(`"size": 11`, `"size": 12`), public, "signature does not match"},
		"changed digest":  {replace(`"sha256": "`, `"sha256": "0`), public, "64 lowercase"},
		"forged signer":   {forgedData, public, "signature does not match"},
		"unknown field": {
			replace(`"name": "beacon",`, `"name": "beacon", "x": 1,`), public, "canonical",
		},
		"compact":          {replace("\n  \"name\"", "\"name\""), public, "canonical"},
		"key case":         {replace(`"version"`, `"Version"`), public, "canonical"},
		"no trailing line": {data[:len(data)-1], public, "canonical"},
		"format":           {replace(Format, "goje-update-v2"), public, "format"},
		"not json":         {[]byte("SHA256SUMS"), public, "decode"},
		"too large":        {make([]byte, MaxManifest+1), public, "more than"},
		"bad signature":    {replace(`"signature": "`, `"signature": "!`), public, "base64"},
	} {
		_, err := Verify(test.data, test.key)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", name, err, test.want)
		}
	}
}

func TestSignRejects(t *testing.T) {
	t.Parallel()
	_, private := testKey(t)
	for name, edit := range map[string]func(*Manifest){
		"name":       func(m *Manifest) { m.Name = "Beacon" },
		"version":    func(m *Manifest) { m.Version = "v1.2.0" },
		"no files":   func(m *Manifest) { m.Files = nil },
		"path":       func(m *Manifest) { m.Files[0].Name = "../x" },
		"dot":        func(m *Manifest) { m.Files[0].Name = ".." },
		"order":      func(m *Manifest) { m.Files[0], m.Files[1] = m.Files[1], m.Files[0] },
		"duplicate":  func(m *Manifest) { m.Files[1].Name = m.Files[0].Name },
		"size":       func(m *Manifest) { m.Files[0].Size = -1 },
		"upper hex":  func(m *Manifest) { m.Files[0].SHA256 = strings.ToUpper(m.Files[0].SHA256) },
		"many files": func(m *Manifest) { m.Files = make([]File, MaxFiles+1) },
		"huge":       func(m *Manifest) { m.Files[0].Size = MaxFileSize + 1 },
		"no digest":  func(m *Manifest) { m.Files[0].SHA256 = "" },
	} {
		m := manifest()
		edit(&m)
		if _, err := Sign(m, private); err == nil {
			t.Errorf("%s: Sign accepted %+v", name, m)
		}
	}
	if _, err := Sign(manifest(), private[:10]); err == nil {
		t.Error("Sign accepted a short private key")
	}
}

func TestNewer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		manifest, current string
		want              bool
	}{
		{"1.2.0", "1.1.9", true},
		{"1.2.0", "1.2.0", false},
		{"1.1.0", "1.2.0", false}, // An old signed manifest is not an update.
		{"1.10.0", "1.9.0", true},
		{"2.0.0", "1.99.99", true},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"1.0.0-rc.10", "1.0.0-rc.9", true},
		{"1.0.0-beta", "1.0.0-alpha.1", true},
		{"1.0.0-alpha.1", "1.0.0-alpha", true},
		{"1.0.0-alpha.beta", "1.0.0-alpha.1", true},
		{"1.0.0-alpha", "1.0.0-alpha.1", false},
	} {
		got, err := Newer(Manifest{Version: test.manifest}, test.current)
		if err != nil || got != test.want {
			t.Errorf("Newer(%s, %s) = %v, %v; want %v", test.manifest, test.current, got, err, test.want)
		}
	}
	for _, bad := range []string{"", "1.2", "v1.2.3", "1.2.3+meta"} {
		if _, err := Newer(Manifest{Version: "1.0.0"}, bad); err == nil {
			t.Errorf("Newer accepted current version %q", bad)
		}
	}
}

func TestDownload(t *testing.T) {
	t.Parallel()
	served := map[string][]byte{
		"/good": webZip, "/changed": []byte("web archivE"), "/long": append(bytes.Clone(webZip), '!'),
		"/short": webZip[:5],
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := served[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Has("chunked") {
			w.(http.Flusher).Flush() // No Content-Length, so the size check runs while reading.
		}
		_, _ = w.Write(data)
		if r.URL.Query().Has("abort") {
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // Omit the final chunk after sending every signed byte.
		}
	}))
	defer server.Close()
	file := fileOf("beacon-1.2.0-web.zip", webZip)
	var out bytes.Buffer
	if err := Download(t.Context(), server.Client(), server.URL+"/good", file, &out); err != nil ||
		!bytes.Equal(out.Bytes(), webZip) {
		t.Fatalf("good download = %q, %v", out.Bytes(), err)
	}
	for _, test := range []struct{ path, want string }{
		{"/changed", "digest does not match"},
		{"/long", "more than 11"},
		{"/long?chunked", "more than 11 bytes"},
		{"/good?chunked&abort", "unexpected EOF"},
		{"/short", "5 bytes, want 11"},
		{"/missing", "404"},
	} {
		err := Download(t.Context(), server.Client(), server.URL+test.path, file, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", test.path, err, test.want)
		}
	}
	bad := file
	bad.SHA256 = "x"
	if err := Download(t.Context(), server.Client(), server.URL+"/good", bad, &out); err == nil {
		t.Error("Download accepted an invalid digest")
	}
}

func TestParsePublicKey(t *testing.T) {
	t.Parallel()
	public, private := testKey(t)
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePublicKey(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if err != nil || !got.Equal(public) {
		t.Fatalf("ParsePublicKey = %x, %v", got, err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKey(pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})); err == nil {
		t.Error("ParsePublicKey accepted a private key")
	}
	if _, err := ParsePublicKey([]byte("not pem")); err == nil {
		t.Error("ParsePublicKey accepted text")
	}
}

func FuzzVerify(f *testing.F) {
	public, private := testKey(f)
	data, err := Sign(manifest(), private)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte("{}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Verify(data, public)
		if err != nil {
			return
		}
		// Anything accepted must be what Sign writes, so only the key holder made it.
		again, err := Sign(m, private)
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("accepted non-canonical manifest %q", data)
		}
	})
}

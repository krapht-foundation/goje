package update

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// channel serves a signed manifest at /channel/update.json and each archive at
// /v{version}/{file}, as a hosted channel does, over HTTPS.
func channel(t *testing.T, served map[string][]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plain" {
			http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
			return
		}
		data, ok := served[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCheck(t *testing.T) {
	t.Parallel()
	public, private := testKey(t)
	otherPublic, _ := testKey(t)
	good, err := Sign(manifest(), private)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(good, []byte("1.2.0"), []byte("1.3.0"), 1)
	server := channel(t, map[string][]byte{"/channel/update.json": good, "/tampered": tampered})
	url := server.URL + "/channel/update.json"
	m, newer, err := Check(t.Context(), server.Client(), url, public, "beacon", "1.1.9")
	if err != nil || !newer || m.Version != "1.2.0" || len(m.Files) != 2 {
		t.Fatalf("Check = %+v, %v, %v", m, newer, err)
	}
	for _, current := range []string{"1.2.0", "1.2.1", "2.0.0"} {
		_, newer, err := Check(t.Context(), server.Client(), url, public, "beacon", current)
		if err != nil || newer {
			t.Errorf("current %s: newer = %v, %v; an older manifest must not be an update",
				current, newer, err)
		}
	}
	for _, test := range []struct {
		name, url, game, want string
		key                   []byte
	}{
		{"wrong key", url, "beacon", "signature does not match", otherPublic},
		{"wrong name", url, "outpost", `not "outpost"`, public},
		{"tampered", server.URL + "/tampered", "beacon", "signature does not match", public},
		{"missing", server.URL + "/missing", "beacon", "404", public},
		{"plain http", strings.Replace(url, "https:", "http:", 1), "beacon", "not an https URL", public},
		{"plain redirect", server.URL + "/plain", "beacon", "not https", public},
	} {
		_, _, err := Check(t.Context(), server.Client(), test.url, test.key, test.game, "1.0.0")
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", test.name, err, test.want)
		}
	}
}

func TestFileURL(t *testing.T) {
	t.Parallel()
	const pattern = "https://example.com/releases/download/beacon-staging-v{version}/{file}"
	got, file, err := FileURL(pattern, manifest(), "beacon-1.2.0-web.zip")
	if err != nil || file != fileOf("beacon-1.2.0-web.zip", webZip) ||
		got != "https://example.com/releases/download/beacon-staging-v1.2.0/beacon-1.2.0-web.zip" {
		t.Fatalf("FileURL = %q, %+v, %v", got, file, err)
	}
	for _, test := range []struct{ pattern, name, want string }{
		{pattern, "beacon-1.2.0-windows-amd64.zip", "has no file"},
		{"https://example.com/latest/{file}", "beacon-1.2.0-web.zip", "{version} and {file}"},
		{"https://example.com/{version}/{version}/{file}", "beacon-1.2.0-web.zip", "once each"},
		{"http://example.com/{version}/{file}", "beacon-1.2.0-web.zip", "not an https URL"},
	} {
		if _, _, err := FileURL(test.pattern, manifest(), test.name); err == nil ||
			!strings.Contains(err.Error(), test.want) {
			t.Errorf("%s %s: error = %v, want %q", test.pattern, test.name, err, test.want)
		}
	}
}

func TestSave(t *testing.T) {
	t.Parallel()
	file := fileOf("beacon-1.2.0-web.zip", webZip)
	server := channel(t, map[string][]byte{
		"/v1.2.0/" + file.Name: webZip, "/changed": []byte("web archivE"), "/short": webZip[:3],
	})
	dir := t.TempDir()
	path, saveErr := Save(t.Context(), server.Client(), server.URL+"/v1.2.0/"+file.Name, file, dir)
	if saveErr != nil || path != filepath.Join(dir, file.Name) {
		t.Fatalf("Save = %q, %v", path, saveErr)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, webZip) {
		t.Fatalf("saved %q, %v", data, err)
	}
	// A second save never replaces the published file.
	if _, err := Save(
		t.Context(),
		server.Client(),
		server.URL+"/v1.2.0/"+file.Name,
		file,
		dir,
	); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Errorf("second Save error = %v", err)
	}
	for _, path := range []string{"/changed", "/short", "/missing", "/plain"} {
		empty := t.TempDir()
		if _, err := Save(t.Context(), server.Client(), server.URL+path, file, empty); err == nil {
			t.Errorf("%s: Save accepted it", path)
		}
		if names, err := os.ReadDir(empty); err != nil || len(names) != 0 {
			t.Errorf("%s: left %v, %v; want an empty directory", path, names, err)
		}
	}
	bad := file
	bad.Name = "../escape"
	if _, err := Save(t.Context(), server.Client(), server.URL+"/changed", bad, dir); err == nil {
		t.Error("Save accepted a path as a file name")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("directory holds %v, %v; want only the verified file", entries, err)
	}
}

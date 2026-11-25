package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/update"
)

// releaseFixture writes a key pair, a second public key, and the archives of
// beacon 1.2.0 for the web, linux-amd64, and windows-amd64.
func releaseFixture(t *testing.T) (dir, key, pub, otherPub string, archives []string) {
	t.Helper()
	dir = t.TempDir()
	for _, prefix := range []string{"signing", "other"} {
		if err := keygen([]string{"-o", filepath.Join(dir, prefix)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"beacon-1.2.0-web.zip", "beacon-1.2.0-linux-amd64.tar.gz", "beacon-1.2.0-windows-amd64.zip",
	} {
		archive := filepath.Join(dir, name)
		if err := os.WriteFile(archive, []byte("archive "+name), 0o600); err != nil {
			t.Fatal(err)
		}
		archives = append(archives, archive)
	}
	return dir, filepath.Join(dir, "signing.key"), filepath.Join(dir, "signing.pub"),
		filepath.Join(dir, "other.pub"), archives
}

func TestManifest(t *testing.T) {
	t.Parallel()
	dir, key, pub, otherPub, archives := releaseFixture(t)
	out := filepath.Join(dir, "update.json")
	args := func(pub, out string, archives ...string) []string {
		return append([]string{
			"-name", "beacon", "-version", "1.2.0", "-sign", key, "-pub", pub,
			"-platforms", "linux-amd64,windows-amd64", "-o", out,
		}, archives...)
	}
	if err := manifest(args(pub, out, archives...)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	pubData, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}
	public, err := update.ParsePublicKey(pubData)
	if err != nil {
		t.Fatal(err)
	}
	m, err := update.Verify(data, public)
	if err != nil || m.Name != "beacon" || m.Version != "1.2.0" || len(m.Files) != 3 {
		t.Fatalf("Verify = %+v, %v", m, err)
	}
	// The same base name from another directory is a duplicate.
	copyDir := t.TempDir()
	duplicate := filepath.Join(copyDir, filepath.Base(archives[0]))
	if err := os.WriteFile(duplicate, []byte("other bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(copyDir, "beacon-1.1.0-web.zip")
	if err := os.WriteFile(renamed, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, want string
		args       []string
	}{
		{"existing output", "exists", args(pub, out, archives...)},
		{"wrong public key", "does not match", args(otherPub, filepath.Join(dir, "a"), archives...)},
		{"missing archive", "are not exactly", args(pub, filepath.Join(dir, "b"), archives[:2]...)},
		{
			"duplicate", "are not exactly",
			args(pub, filepath.Join(dir, "c"), append(archives, duplicate)...),
		},
		{
			"other version", "are not exactly",
			args(pub, filepath.Join(dir, "d"), renamed, archives[1], archives[2]),
		},
		{"no archives", "invalid arguments", args(pub, filepath.Join(dir, "e"))},
	} {
		err := manifest(test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", test.name, err, test.want)
		}
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("a failed manifest left %s", name)
		}
	}
	if after, err := os.ReadFile(out); err != nil || !bytes.Equal(after, data) {
		t.Error("a failed manifest changed the existing update.json")
	}
}

// TestCheckUpdate is not parallel because it sets updateClient.
func TestCheckUpdate(t *testing.T) {
	dir, key, pub, otherPub, archives := releaseFixture(t)
	manifestFile := filepath.Join(dir, "update.json")
	if err := manifest(append([]string{
		"-name", "beacon", "-version", "1.2.0", "-sign", key,
		"-pub", pub, "-platforms", "linux-amd64,windows-amd64", "-o", manifestFile,
	}, archives...)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	updateClient = server.Client()
	defer func() { updateClient = nil }()
	out := t.TempDir()
	base := []string{
		"-manifest", server.URL + "/update.json", "-artifacts", server.URL + "/{file}?v={version}",
		"-name", "beacon",
	}
	run := func(extra ...string) (string, error) {
		var log strings.Builder
		err := checkUpdate(t.Context(), append(append([]string{}, base...), extra...), &log)
		return log.String(), err
	}
	if log, err := run("-key", pub, "-current", "1.1.0"); err != nil ||
		!strings.Contains(log, "verified beacon 1.2.0") || !strings.Contains(log, "linux-amd64") {
		t.Fatalf("check = %q, %v", log, err)
	}
	if log, err := run("-key", pub, "-current", "1.1.0", "-file", "beacon-1.2.0-linux-amd64.tar.gz",
		"-o", out); err != nil || !strings.Contains(log, "saved ") {
		t.Fatalf("download = %q, %v", log, err)
	}
	for _, test := range []struct {
		want  string
		extra []string
	}{
		{"not newer", []string{"-key", pub, "-current", "1.2.0"}},
		{"signature does not match", []string{"-key", otherPub, "-current", "1.1.0"}},
		{"has no file", []string{
			"-key", pub, "-current", "1.1.0", "-file", "beacon-1.2.0-darwin-arm64.tar.gz",
			"-o", t.TempDir(),
		}},
		{"invalid arguments", []string{"-key", pub, "-current", "1.1.0", "-o", t.TempDir()}},
	} {
		if _, err := run(test.extra...); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%v: error = %v, want %q", test.extra, err, test.want)
		}
	}
	// A changed archive is refused and leaves nothing behind.
	if err := os.WriteFile(archives[0], []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	if _, err := run("-key", pub, "-current", "1.1.0", "-file", "beacon-1.2.0-web.zip",
		"-o", empty); err == nil {
		t.Error("downloaded a changed archive")
	}
	if names, err := os.ReadDir(empty); err != nil || len(names) != 0 {
		t.Errorf("a failed download left %v, %v", names, err)
	}
}

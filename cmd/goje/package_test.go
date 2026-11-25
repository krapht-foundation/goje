package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeBuild writes a build output with a deliberately unsorted creation order and odd modes.
func fakeBuild(tb testing.TB, out string) {
	tb.Helper()
	files := []struct {
		name string
		mode os.FileMode
	}{
		{"web/manifest.webmanifest", 0o600},
		{"web/index.html", 0o640},
		{"web/game.wasm", 0o755},
		{"desktop/linux-amd64/game", 0o700},
		{"desktop/linux-amd64/game.png", 0o644},
		{"desktop/linux-amd64/game.desktop", 0o600},
		{"desktop/linux-amd64/data/z.txt", 0o644},
		{"desktop/linux-amd64/data/a.txt", 0o644},
	}
	for _, file := range files {
		path := filepath.Join(out, file.name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file.name), 0o600); err != nil {
			tb.Fatal(err)
		}
		if err := os.Chmod(path, file.mode); err != nil {
			tb.Fatal(err)
		}
	}
}

func readRelease(tb testing.TB, out string) map[string][]byte {
	tb.Helper()
	files := map[string][]byte{}
	for _, name := range []string{
		"game-1.0.0-web.zip", "game-1.0.0-linux-amd64.tar.gz", "SHA256SUMS",
	} {
		data, err := os.ReadFile(filepath.Join(out, "release", name))
		if err != nil {
			tb.Fatal(err)
		}
		files[name] = data
	}
	return files
}

func TestPackDeterministic(t *testing.T) {
	t.Parallel()
	first, second := t.TempDir(), t.TempDir()
	fakeBuild(t, first)
	modTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := pack(first, "game", "1.0.0", "linux-amd64", modTime); err != nil {
		t.Fatal(err)
	}
	want := readRelease(t, first)
	// Rebuilding elsewhere with new file times must give the same bytes.
	time.Sleep(10 * time.Millisecond)
	fakeBuild(t, second)
	if err := pack(second, "game", "1.0.0", "linux-amd64", modTime); err != nil {
		t.Fatal(err)
	}
	for name, data := range readRelease(t, second) {
		if !bytes.Equal(data, want[name]) {
			t.Errorf("%s differs between two packages of the same build", name)
		}
	}
	entries, err := os.ReadDir(filepath.Join(first, "release"))
	if err != nil || len(entries) != 3 {
		t.Fatalf("release has %d entries (%v), want 3", len(entries), err)
	}

	// Checksums are sorted, in the sha256sum text format, and match the files.
	var sums strings.Builder
	for _, name := range []string{"game-1.0.0-linux-amd64.tar.gz", "game-1.0.0-web.zip"} {
		hash := sha256.Sum256(want[name])
		sums.WriteString(hex.EncodeToString(hash[:]) + "  " + name + "\n")
	}
	if got := string(want["SHA256SUMS"]); got != sums.String() {
		t.Errorf("SHA256SUMS = %q, want %q", got, sums.String())
	}
	if !regexp.MustCompile(`^([0-9a-f]{64} {2}\S+\n)+$`).Match(want["SHA256SUMS"]) {
		t.Error("SHA256SUMS is not in the sha256sum format")
	}

	checkZip(t, want["game-1.0.0-web.zip"], modTime)
	checkTar(t, want["game-1.0.0-linux-amd64.tar.gz"], modTime)
}

func checkZip(tb testing.TB, data []byte, modTime time.Time) {
	tb.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		tb.Fatal(err)
	}
	got := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		if !file.Modified.Equal(modTime) || file.Method != zip.Deflate {
			tb.Errorf("%s: time %v method %d", file.Name, file.Modified, file.Method)
		}
		got = append(got, file.Name+" "+file.Mode().String())
		checkContent(tb, file.Open, "web/"+file.Name)
	}
	want := []string{
		"game.wasm -rwxr-xr-x", "index.html -rw-r--r--", "manifest.webmanifest -rw-r--r--",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		tb.Errorf("zip entries = %q, want %q", got, want)
	}
}

func checkTar(tb testing.TB, data []byte, modTime time.Time) {
	tb.Helper()
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		tb.Fatal(err)
	}
	if !compressed.ModTime.IsZero() || compressed.Name != "" {
		tb.Errorf("gzip header time %v name %q, want neither", compressed.ModTime, compressed.Name)
	}
	archive := tar.NewReader(compressed)
	var got []string
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			tb.Fatal(err)
		}
		if !header.ModTime.Equal(modTime) || header.Uid != 0 || header.Gid != 0 ||
			header.Uname != "" || header.Gname != "" || len(header.PAXRecords) != 0 {
			tb.Errorf(
				"%s: header %+v carries more than a name, mode, size, and time",
				header.Name,
				header,
			)
		}
		got = append(got, header.Name+" "+header.FileInfo().Mode().String())
		if header.Typeflag == tar.TypeReg {
			checkContent(tb, func() (io.ReadCloser, error) { return io.NopCloser(archive), nil },
				"desktop/linux-amd64/"+header.Name)
		}
	}
	want := []string{
		"data/ drwxr-xr-x", "data/a.txt -rw-r--r--", "data/z.txt -rw-r--r--",
		"game -rwxr-xr-x", "game.desktop -rw-r--r--", "game.png -rw-r--r--",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		tb.Errorf("tar entries = %q, want %q", got, want)
	}
}

// checkContent compares an archived file with what fakeBuild wrote at name.
func checkContent(tb testing.TB, open func() (io.ReadCloser, error), name string) {
	tb.Helper()
	reader, err := open()
	if err != nil {
		tb.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != name {
		tb.Errorf("%s holds %q (%v)", name, data, err)
	}
}

func TestPackWebOnly(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	fakeBuild(t, out)
	// A stale release file from an earlier run must not survive.
	if err := os.MkdirAll(filepath.Join(out, "release"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "release", "old.zip"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pack(out, "game", "1.0.0", "", defaultDate); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(out, "release"))
	if err != nil || len(entries) != 2 || entries[0].Name() != "SHA256SUMS" ||
		entries[1].Name() != "game-1.0.0-web.zip" {
		t.Fatalf("release entries = %v (%v), want SHA256SUMS and the web zip", entries, err)
	}
}

func TestPackRejectsSymlinks(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	fakeBuild(t, out)
	if err := os.Symlink("/etc/passwd", filepath.Join(out, "web", "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := pack(out, "game", "1.0.0", "", defaultDate); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestSourceDate(t *testing.T) {
	t.Parallel()
	if got, err := sourceDate(""); err != nil || !got.Equal(defaultDate) {
		t.Errorf("unset = %v, %v", got, err)
	}
	if got, err := sourceDate("1700000000"); err != nil || got.Unix() != 1700000000 ||
		got.Location() != time.UTC {
		t.Errorf("set = %v, %v", got, err)
	}
	for _, bad := range []string{"x", "-1", "1.5"} {
		if _, err := sourceDate(bad); err == nil {
			t.Errorf("sourceDate(%q) accepted", bad)
		}
	}
}

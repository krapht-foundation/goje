//go:build !(js && wasm)

package platform

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "goje", "game")
	store, err := newDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("slot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: got %v", err)
	}
	for _, value := range [][]byte{[]byte("first"), {}, bytes.Repeat([]byte{7}, MaxStoreValueSize)} {
		if err := store.Put("slot", value); err != nil {
			t.Fatal(err)
		}
		got, err := store.Get("slot")
		if err != nil || !bytes.Equal(got, value) {
			t.Fatalf("got %d bytes, %v; want %d bytes", len(got), err, len(value))
		}
	}
	if err := store.Delete("slot"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("slot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted key: got %v", err)
	}
	if err := store.Delete("slot"); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("directory mode %v, %v", info.Mode(), err)
		}
		if err := store.Put("mode-check", []byte("x")); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(
			filepath.Join(dir, "mode-check.bin"),
		); err != nil ||
			info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v, %v", info.Mode(), err)
		}
	}
}

func TestDirStoreRejects(t *testing.T) {
	store, err := newDirStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("big", make([]byte, MaxStoreValueSize+1)); err == nil {
		t.Error("accepted an oversized value")
	}
	for _, key := range []string{"", "Slot", "1slot", "-slot", "a.b", "a/b", "../x", strings.Repeat("k", 33)} {
		if store.Put(key, nil) == nil {
			t.Errorf("Put accepted key %q", key)
		}
		if _, err := store.Get(key); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("Get accepted key %q: %v", key, err)
		}
		if store.Delete(key) == nil {
			t.Errorf("Delete accepted key %q", key)
		}
	}
	if err := store.Put("a_"+strings.Repeat("9", 29)+"-", nil); err != nil {
		t.Errorf("rejected a valid 32-byte key: %v", err)
	}
	// A file that grew outside the store is refused rather than read whole.
	if err := os.WriteFile(
		store.path("grown"),
		make([]byte, MaxStoreValueSize+1),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("grown"); err == nil {
		t.Error("read an oversized file")
	}
	for _, game := range []string{"", "Game", "has-hyphen", strings.Repeat("g", 33)} {
		if _, err := NewStore(game); err == nil {
			t.Errorf("NewStore accepted game %q", game)
		}
	}
}

func TestDirStoreFailedPutKeepsValue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block writes on Windows")
	}
	dir := t.TempDir()
	store, err := newDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put("slot", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil {
			t.Error(chmodErr)
		}
	})
	if store.Put("slot", []byte("new")) == nil {
		t.Skip("the directory stayed writable, as it does for root")
	}
	got, err := store.Get("slot")
	if err != nil || string(got) != "old" {
		t.Fatalf("after a failed write: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory holds %d entries, %v", len(entries), err)
	}
}

func TestDirStoreRemovedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	store, err := newDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if store.Put("slot", []byte("x")) == nil {
		t.Fatal("wrote into a removed directory")
	}
	if _, err := store.Get("slot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestNewStoreUsesConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))
	config, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	store, err := NewStore("outpost")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("save", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config, "goje", "outpost", "save.bin")); err != nil {
		t.Fatal(err)
	}
}

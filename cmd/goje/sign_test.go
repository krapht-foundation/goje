package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/update"
)

func TestKeygen(t *testing.T) {
	t.Parallel()
	prefix := filepath.Join(t.TempDir(), "release")
	if err := keygen([]string{"-o", prefix}); err != nil {
		t.Fatal(err)
	}
	private, err := readPrivateKey(prefix + ".key")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	public, err := update.ParsePublicKey(data)
	if err != nil || !public.Equal(private.Public()) {
		t.Fatalf("public key %x does not match the private key (%v)", public, err)
	}
	info, err := os.Stat(prefix + ".key")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %v, want 0600", info.Mode())
	}
	// An existing key pair is never replaced, and a failed run leaves no new file.
	if err = keygen([]string{"-o", prefix}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second keygen error = %v, want an existing file", err)
	}
	again, err := readPrivateKey(prefix + ".key")
	if err != nil || !again.Equal(private) {
		t.Fatal("keygen replaced the private key")
	}
	half := filepath.Join(t.TempDir(), "half")
	if err := os.WriteFile(half+".pub", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keygen([]string{"-o", half}); err == nil {
		t.Fatal("keygen replaced an existing public key")
	}
	if _, err := os.Stat(half + ".key"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("keygen left a private key without its public key (%v)", err)
	}
	for _, args := range [][]string{{"-o", ""}, {"extra"}, {"-x"}} {
		if err := keygen(args); !errors.Is(err, errUsage) {
			t.Errorf("keygen(%q) error = %v, want usage", args, err)
		}
	}
}

func TestReadPrivateKeyRejects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		data []byte
		want string
	}{
		"not pem": {[]byte("key"), "PEM PRIVATE KEY"},
		"public": {
			pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), "PEM PRIVATE KEY",
		},
		"corrupt": {
			pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der[:10]}), "signing key",
		},
		"empty file": {nil, "PEM PRIVATE KEY"},
	} {
		file := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(file, test.data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrivateKey(file); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", name, err, test.want)
		}
	}
	if _, err := readPrivateKey(filepath.Join(dir, "missing")); err == nil {
		t.Error("readPrivateKey accepted a missing file")
	}
}

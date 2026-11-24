//go:build !wasm

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSymlinks(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"version":1,"level":"level.json","ground":"ground.png"}`)
	outside := filepath.Join(parent, "outside.json")
	if err := os.WriteFile(outside, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escaping": "../outside.json", "absolute": outside} {
		t.Run(name, func(t *testing.T) {
			link := filepath.Join(root, "goje.json")
			if err := os.Symlink(target, link); err != nil {
				if os.IsPermission(err) {
					t.Skipf("cannot create symlink: %v", err)
				}
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Remove(link); err != nil {
					t.Error(err)
				}
			})
			if err := check(root); err == nil || !strings.Contains(err.Error(), `stat "goje.json"`) {
				t.Fatalf("check error = %v, want rejection at manifest symlink", err)
			}
		})
	}
	t.Run("relative inside root", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("manifest.json", filepath.Join(root, "goje.json")); err != nil {
			if os.IsPermission(err) {
				t.Skipf("cannot create symlink: %v", err)
			}
			t.Fatal(err)
		}
		if err := check(root); err == nil || !strings.Contains(err.Error(), `stat "level.json"`) {
			t.Fatalf("check error = %v, want missing level after following manifest symlink", err)
		}
	})
}

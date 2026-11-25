package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	opts, err := parse([]string{"package"}, "linux", &log)
	if err != nil {
		t.Fatal(err)
	}
	want := options{
		command: "package",
		out:     "dist",
		version: "0.0.0",
		pkg:     ".",
		web:     true,
		desktop: true,
	}
	if opts != want {
		t.Fatalf("defaults = %+v, want %+v", opts, want)
	}
	opts, err = parse([]string{
		"build", "-o", "out", "-name", "my-game2", "-version", "1.20.3-rc.1", "-id", "org.example.Game",
		"-tags", "a,b", "-icon", "i.png", "-target", "web", "./game",
	}, "darwin", &log)
	if err != nil {
		t.Fatal(err)
	}
	want = options{
		command: "build", out: "out", name: "my-game2", version: "1.20.3-rc.1", id: "org.example.Game",
		tags: "a,b", icon: "i.png", pkg: "./game", web: true,
	}
	if opts != want {
		t.Fatalf("flags = %+v, want %+v", opts, want)
	}
	if log.Len() != 0 {
		t.Fatalf("unexpected log %q", log.String())
	}
	opts, err = parse([]string{"build"}, "windows", &log)
	if err != nil || !opts.web || opts.desktop ||
		!strings.Contains(log.String(), "only for the web") {
		t.Fatalf("all on windows = %+v, %v, log %q; want web only with a note", opts, err, log.String())
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		args []string
		goos string
		want string
	}{
		{nil, "linux", "first argument"},
		{[]string{"run"}, "linux", "first argument"},
		{[]string{"build", "-x"}, "linux", "not defined"},
		{[]string{"build", "a", "b"}, "linux", "at most one package"},
		{[]string{"build", "-o", ""}, "linux", "-o must not be empty"},
		{[]string{"build", "-name", "Game"}, "linux", "-name"},
		{[]string{"build", "-name", "9lives"}, "linux", "-name"},
		{[]string{"build", "-name", strings.Repeat("a", 33)}, "linux", "-name"},
		{[]string{"build", "-version", "1.2"}, "linux", "-version"},
		{[]string{"build", "-version", "v1.2.3"}, "linux", "-version"},
		{[]string{"build", "-version", "01.2.3"}, "linux", "-version"},
		{[]string{"build", "-version", "1.2.3+build"}, "linux", "-version"},
		{[]string{"build", "-id", "game"}, "linux", "-id"},
		{[]string{"build", "-id", "com.example/game"}, "linux", "-id"},
		{[]string{"build", "-tags", "a b"}, "linux", "-tags"},
		{[]string{"build", "-target", "android"}, "linux", "-target"},
	} {
		_, err := parse(test.args, test.goos, &bytes.Buffer{})
		if !errors.Is(err, errUsage) || !strings.Contains(err.Error(), test.want) {
			t.Errorf("parse(%q) error = %v, want usage error mentioning %q", test.args, err, test.want)
		}
	}
	for _, goos := range []string{"windows", "freebsd", "js"} {
		_, err := parse([]string{"package", "-target", "desktop"}, goos, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "supports only darwin and linux") ||
			!strings.Contains(err.Error(), goos) {
			t.Errorf("desktop on %s error = %v, want unsupported host", goos, err)
		}
	}
}

func TestResolveRejects(t *testing.T) {
	opts := options{command: "build", out: t.TempDir(), version: "0.0.0", web: true}
	opts.pkg = "github.com/krapht-foundation/goje/project"
	if _, err := resolve(t.Context(), opts); err == nil ||
		!strings.Contains(err.Error(), "not a main package") {
		t.Fatalf("library package error = %v", err)
	}
	opts.pkg = "github.com/krapht-foundation/goje/cmd/..."
	if _, err := resolve(t.Context(), opts); err == nil ||
		!strings.Contains(err.Error(), "exactly one package") {
		t.Fatalf("pattern error = %v", err)
	}
	opts.pkg = "../playground"
	opts.icon = "shell/index.html"
	if _, err := resolve(t.Context(), opts); err == nil || !strings.Contains(err.Error(), "PNG") {
		t.Fatalf("icon error = %v", err)
	}
	opts.icon, opts.desktop = "", true
	t.Setenv("GOOS", "windows")
	if _, err := resolve(t.Context(), opts); err == nil ||
		!strings.Contains(err.Error(), "only for the host system") {
		t.Fatalf("cross desktop error = %v", err)
	}
}

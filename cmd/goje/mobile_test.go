package main

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseMobile(t *testing.T) {
	t.Parallel()
	opts, err := parseMobile([]string{
		"-id", "io.example.beacon", "-build", "7",
		"-version", "1.2.3-rc.1", "-platform", "ios", "./game",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.id != "io.example.beacon" || opts.build != 7 || opts.version != "1.2.3-rc.1" ||
		!slices.Equal(opts.platforms, []string{"ios"}) ||
		opts.pkg != "./game" || !opts.web || opts.desktop {
		t.Fatalf("flags = %+v", opts)
	}
	opts, err = parseMobile([]string{"-id", "io.example.beacon", "-build", "1"})
	if err != nil || !slices.Equal(opts.platforms, []string{"android", "ios"}) || opts.out != "dist" {
		t.Fatalf("defaults = %+v, %v", opts, err)
	}
	for _, args := range [][]string{
		{"-build", "1"},
		{"-id", "io.example.beacon"},
		{"-id", "io.example.beacon", "-build", "0"},
		{"-id", "io.example.beacon", "-build", "2100000001"},
		{"-id", "beacon", "-build", "1"},
		{"-id", "io.example.my-game", "-build", "1"},
		{"-id", "io.example.my_game", "-build", "1"},
		{"-id", "io.example.2d", "-build", "1"},
		{"-id", "io.new.beacon", "-build", "1"},
		{"-id", "io.example.beacon", "-build", "1", "-platform", "web"},
		{"-id", "io.example.beacon", "-build", "1", "-version", "v1.0.0"},
		{"-id", "io.example.beacon", "-build", "1", "a", "b"},
	} {
		if _, err := parseMobile(args); !errors.Is(err, errUsage) {
			t.Errorf("parseMobile(%q) = %v, want a usage error", args, err)
		}
	}
}

// The lines of Capacitor 8.6.2's templates that goje mobile reads and changes.
const (
	gradleTemplate = `    defaultConfig {
        applicationId "io.example.beacon"
        versionCode 1
        versionName "1.0"
    }
`
	pbxTemplate = `				CURRENT_PROJECT_VERSION = 1;
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = io.example.beacon;
				CURRENT_PROJECT_VERSION = 1;
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = io.example.beacon;
`
)

func writeTestFile(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(file) // #nosec G304 -- Test scratch file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSyncAndroid(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "android")
	g := &game{id: "io.example.beacon", version: "2.0.1-beta.1", png: defaultIcon}
	if err := g.checkProjectID(dir); err != nil {
		t.Fatalf("a missing project = %v", err)
	}
	gradle := filepath.Join(dir, "app", "build.gradle")
	writeTestFile(t, gradle, gradleTemplate)
	res := filepath.Join(dir, "app", "src", "main", "res")
	writeTestFile(t, filepath.Join(res, "mipmap-hdpi", "ic_launcher.png"), "old")
	writeTestFile(t, filepath.Join(res, "values", "strings.xml"), "native")
	if err := g.checkProjectID(dir); err != nil {
		t.Fatal(err)
	}
	if err := g.syncAndroid(dir, 42); err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer("versionCode 1", "versionCode 42",
		`versionName "1.0"`, `versionName "2.0.1-beta.1"`).Replace(gradleTemplate)
	if got := readTestFile(t, gradle); got != want {
		t.Fatalf("build.gradle =\n%s\nwant\n%s", got, want)
	}
	densityIcon := filepath.Join(res, "mipmap-hdpi", "ic_launcher.png")
	if _, err := os.Stat(densityIcon); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the template's density icon remains: %v", err)
	}
	foreground := readTestFile(t, filepath.Join(res, "mipmap-xxxhdpi", "ic_launcher_foreground.png"))
	adaptive := readTestFile(t, filepath.Join(res, "mipmap-anydpi-v26", "ic_launcher.xml"))
	if foreground != string(defaultIcon) || !strings.Contains(adaptive, "16.667%") ||
		readTestFile(t, filepath.Join(res, "values", "strings.xml")) != "native" {
		t.Fatal("unexpected launcher resources")
	}
	other := &game{id: "io.example.other"}
	err := other.checkProjectID(dir)
	if err == nil || !strings.Contains(err.Error(), "io.example.beacon") {
		t.Fatalf("a different ID = %v", err)
	}
}

func TestSyncIOS(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "ios")
	g := &game{id: "io.example.beacon", version: "2.0.1-beta.1", png: defaultIcon}
	pbx := filepath.Join(dir, "App", "App.xcodeproj", "project.pbxproj")
	writeTestFile(t, pbx, pbxTemplate)
	if err := g.checkProjectID(dir); err != nil {
		t.Fatal(err)
	}
	if err := g.syncIOS(dir, 42); err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer("CURRENT_PROJECT_VERSION = 1;", "CURRENT_PROJECT_VERSION = 42;",
		"MARKETING_VERSION = 1.0;", "MARKETING_VERSION = 2.0.1;").Replace(pbxTemplate)
	if got := readTestFile(t, pbx); got != want {
		t.Fatalf("project.pbxproj =\n%s\nwant\n%s", got, want)
	}
	icon := readTestFile(t, filepath.Join(dir, "App", "App", "Assets.xcassets", "AppIcon.appiconset",
		"AppIcon-512@2x.png"))
	config, err := png.DecodeConfig(strings.NewReader(icon))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1024 || config.Height != 1024 {
		t.Fatalf("App Store icon is %d × %d", config.Width, config.Height)
	}
	writeTestFile(t, pbx, strings.ReplaceAll(pbxTemplate, "io.example.beacon;", "io.example.other;"))
	if err = g.checkProjectID(dir); err == nil {
		t.Fatal("accepted a different bundle ID")
	}
}

func TestAppStoreIcon(t *testing.T) {
	t.Parallel()
	data, err := appStoreIcon(defaultIcon)
	if err != nil {
		t.Fatal(err)
	}
	// Color type 2 is truecolor without alpha.
	if data[25] != 2 {
		t.Fatalf("PNG color type %d, want 2", data[25])
	}
	src, err := png.Decode(bytes.NewReader(defaultIcon))
	if err != nil {
		t.Fatal(err)
	}
	out, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]int{{256, 256}, {300, 200}, {511, 511}} {
		r, g, b, a := src.At(p[0], p[1]).RGBA()
		if a != 0xffff {
			continue
		}
		if r2, g2, b2, _ := out.At(p[0]*2+1, p[1]*2+1).RGBA(); r != r2 || g != g2 || b != b2 {
			t.Errorf("pixel %v changed", p)
		}
	}
}

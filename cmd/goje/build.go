package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/krapht-foundation/goje/project"
)

const background = "#141c29"

var (
	//go:embed shell/index.html
	shellSource string
	shell       = template.Must(template.New("index.html").Parse(shellSource))
	// The default icon is the Goje brand's 512 × 512 store icon.
	//go:embed shell/icon-512.png
	defaultIcon []byte
)

// game is a resolved build request.
type game struct {
	options
	dir      string // Package source directory.
	png      []byte // Validated 512 × 512 icon.
	platform string // Desktop GOOS-GOARCH, empty without a desktop build.
	goroot   string
}

// resolve checks the package, its project, the icon, and the toolchain's target.
func resolve(ctx context.Context, opts options) (*game, error) {
	listed, err := goOutput(ctx, "list", "-tags="+opts.tags,
		"-f", "{{.Name}}\t{{.ImportPath}}\t{{.Dir}}", "--", opts.pkg)
	if err != nil {
		return nil, err
	}
	listed = strings.TrimSuffix(listed, "\n")
	fields := strings.Split(listed, "\t")
	if len(fields) != 3 || strings.Contains(listed, "\n") {
		return nil, fmt.Errorf("%s must name exactly one package", opts.pkg)
	}
	if fields[0] != "main" {
		return nil, fmt.Errorf("%s is package %s, not a main package", fields[1], fields[0])
	}
	g := &game{options: opts, dir: fields[2]}
	if g.name == "" {
		g.name = path.Base(fields[1])
		if !namePattern.MatchString(g.name) {
			return nil, fmt.Errorf("the name %q from the import path must match [a-z][a-z0-9-]* "+
				"and have at most 32 bytes; set -name", g.name)
		}
	}
	if g.id == "" {
		g.id = "com.example." + g.name
	}
	err = checkProject(g.dir)
	if err != nil {
		return nil, err
	}
	g.png = defaultIcon
	if g.icon != "" {
		if g.png, err = os.ReadFile(g.icon); err != nil { // #nosec G304 G703 -- User-selected file.
			return nil, fmt.Errorf("read icon: %w", err)
		}
	}
	if err = checkIcon(g.png); err != nil {
		return nil, fmt.Errorf("icon %s: %w", g.icon, err)
	}
	env, err := goOutput(ctx, "env", "GOROOT", "GOOS", "GOARCH", "GOHOSTOS", "GOHOSTARCH")
	if err != nil {
		return nil, err
	}
	values := strings.Split(strings.TrimSuffix(env, "\n"), "\n")
	if len(values) != 5 {
		return nil, fmt.Errorf("unexpected go env output %q", env)
	}
	g.goroot = values[0]
	if g.desktop {
		target, host := values[1]+"-"+values[2], values[3]+"-"+values[4]
		if target != host {
			return nil, fmt.Errorf("desktop builds use cgo and run only for the host system %s, "+
				"but GOOS and GOARCH ask for %s; unset them or use -target web", host, target)
		}
		g.platform = host
	}
	return g, nil
}

// checkProject validates goje.json, if the package has one, as cmd/projectcheck does.
func checkProject(dir string) (err error) {
	if _, err = os.Lstat(filepath.Join(dir, "goje.json")); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if _, err = project.Load(root.FS()); err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	return nil
}

// checkIcon accepts only a complete square 512 × 512 PNG, the size an icns ic09 chunk holds.
func checkIcon(data []byte) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("must be a PNG: %w", err)
	}
	if size := img.Bounds().Size(); size.X != 512 || size.Y != 512 {
		return fmt.Errorf("must be 512 × 512 pixels, not %d × %d", size.X, size.Y)
	}
	return nil
}

func (g *game) build(ctx context.Context) error {
	if g.web {
		if err := g.buildWeb(ctx); err != nil {
			return err
		}
	}
	if g.desktop {
		return g.buildDesktop(ctx)
	}
	return nil
}

func (g *game) buildWeb(ctx context.Context) error {
	dir := filepath.Join(g.out, "web")
	if err := fresh(dir); err != nil {
		return err
	}
	if err := g.goBuild(ctx, filepath.Join(dir, "game.wasm"), "GOOS=js", "GOARCH=wasm"); err != nil {
		return err
	}
	// #nosec G703 -- GOROOT comes from the go command.
	support, err := os.ReadFile(filepath.Join(g.goroot, "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		return fmt.Errorf("read the Go WebAssembly support script: %w", err)
	}
	page, err := webPage(g.name)
	if err != nil {
		return err
	}
	manifest, err := webManifest(g.name)
	if err != nil {
		return err
	}
	return writeFiles(dir, map[string][]byte{
		"wasm_exec.js": support, "index.html": page,
		"manifest.webmanifest": manifest, "icon-512.png": g.png,
	})
}

func (g *game) buildDesktop(ctx context.Context) error {
	dir := filepath.Join(g.out, "desktop", g.platform)
	if err := fresh(dir); err != nil {
		return err
	}
	goos, _, _ := strings.Cut(g.platform, "-")
	if goos != "darwin" {
		if err := g.goBuild(ctx, filepath.Join(dir, g.name), "CGO_ENABLED=1"); err != nil {
			return err
		}
		return writeFiles(dir, map[string][]byte{
			g.name + ".desktop": desktopEntry(g.name), g.name + ".png": g.png,
		})
	}
	contents := filepath.Join(dir, g.name+".app", "Contents")
	executable := filepath.Join(contents, "MacOS", g.name)
	if err := g.goBuild(ctx, executable, "CGO_ENABLED=1"); err != nil {
		return err
	}
	if err := writeFiles(contents, map[string][]byte{
		"Info.plist": infoPlist(g.name, g.version, g.id),
	}); err != nil {
		return err
	}
	return writeFiles(filepath.Join(contents, "Resources"), map[string][]byte{
		g.name + ".icns": icns(g.png),
	})
}

// goBuild compiles the package with flags that make the same sources give the same bytes.
func (g *game) goBuild(ctx context.Context, output string, env ...string) error {
	output, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	args := []string{"build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", output}
	if g.tags != "" {
		args = append(args, "-tags="+g.tags)
	}
	args = append(args, "--", g.pkg)
	cmd := exec.CommandContext(ctx, "go", args...) // #nosec G204 G702 -- The developer's own build.
	// exec keeps the last value of a repeated variable, so these override the caller's.
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s for %s: %w", g.pkg, strings.Join(env, " "), err)
	}
	return nil
}

func goOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...) // #nosec G204 -- Fixed go subcommands.
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go %s: %w", args[0], err)
	}
	return string(out), nil
}

// fresh replaces a build output directory so stale files never reach an archive.
// The developer chose the directory with -o.
func fresh(dir string) error {
	if err := os.RemoveAll(dir); err != nil { // #nosec G703 -- Inside the chosen output directory.
		return fmt.Errorf("clear %s: %w", dir, err)
	}
	return writeFiles(dir, nil)
}

func writeFiles(dir string, files map[string][]byte) error {
	// #nosec G301 G703 -- Release output is public and goes where the developer chose.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for name, data := range files {
		file := filepath.Join(dir, name)
		// #nosec G306 G703 -- Release output is public and goes where the developer chose.
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file, err)
		}
	}
	return nil
}

func webPage(name string) ([]byte, error) {
	var page bytes.Buffer
	if err := shell.Execute(&page, name); err != nil {
		return nil, fmt.Errorf("render index.html: %w", err)
	}
	return page.Bytes(), nil
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

// webManifest lets a browser add the game to a home screen.
func webManifest(name string) ([]byte, error) {
	data, err := json.MarshalIndent(struct {
		Name            string         `json:"name"`
		ShortName       string         `json:"short_name"`
		StartURL        string         `json:"start_url"`
		Display         string         `json:"display"`
		BackgroundColor string         `json:"background_color"`
		ThemeColor      string         `json:"theme_color"`
		Icons           []manifestIcon `json:"icons"`
	}{
		name, name, "./", "fullscreen", background, background,
		[]manifestIcon{{"icon-512.png", "512x512", "image/png", "any"}},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode web manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// infoPlist describes the bundle. Name, version, and ID are validated, so they need no escaping.
// Apple bundle versions take only the numeric core of the version.
func infoPlist(name, version, id string) []byte {
	core, _, _ := strings.Cut(version, "-")
	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleDisplayName</key>
	<string>%[1]s</string>
	<key>CFBundleExecutable</key>
	<string>%[1]s</string>
	<key>CFBundleIconFile</key>
	<string>%[1]s.icns</string>
	<key>CFBundleIdentifier</key>
	<string>%[3]s</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>%[1]s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>%[2]s</string>
	<key>CFBundleVersion</key>
	<string>%[2]s</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.games</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`, name, core, id)
}

// icns wraps a 512 × 512 PNG: the magic, the big-endian file length, then one
// ic09 chunk whose length includes its 8-byte header.
func icns(icon []byte) []byte {
	out := make([]byte, 16, 16+len(icon))
	copy(out, "icns")
	binary.BigEndian.PutUint32(out[4:], uint32(len(out)+len(icon))) // #nosec G115 -- Icons are small.
	copy(out[8:], "ic09")
	binary.BigEndian.PutUint32(out[12:], uint32(8+len(icon))) // #nosec G115 -- Icons are small.
	return append(out, icon...)
}

// desktopEntry is the Linux launcher. Exec and Icon name files next to it, which
// the player installs into the PATH and an icon theme as docs/building.md describes.
func desktopEntry(name string) []byte {
	return fmt.Appendf(nil,
		"[Desktop Entry]\nType=Application\nName=%[1]s\nExec=%[1]s\nIcon=%[1]s\n"+
			"Categories=Game;\nTerminal=false\n", name)
}

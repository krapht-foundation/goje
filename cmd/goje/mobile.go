package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	// The pinned Capacitor tools; package-lock.json records every package's digest.
	//go:embed mobile/package.json
	mobilePackage []byte
	//go:embed mobile/package-lock.json
	mobileLock []byte
	// Android application IDs and Java packages allow underscores but no hyphens, and Apple
	// bundle IDs allow hyphens but no underscores, so an ID that suits both uses neither.
	appIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*)+$`)
	// Capacitor makes the application ID the Java package of MainActivity.
	javaKeywords = strings.Fields(`abstract assert boolean break byte case catch char class const
		continue default do double else enum extends false final finally float for goto if
		implements import instanceof int interface long native new null package private protected
		public return short static strictfp super switch synchronized this throw throws transient
		true try void volatile while`)
	versionCodeLine = regexp.MustCompile(`(?m)^(\s*versionCode\s+)\d+\s*$`)
	versionNameLine = regexp.MustCompile(`(?m)^(\s*versionName\s+)"[^"\n]*"\s*$`)
	applicationID   = regexp.MustCompile(`(?m)^\s*applicationId\s+"([^"\n]*)"\s*$`)
	marketingLine   = regexp.MustCompile(`(?m)^(\s*MARKETING_VERSION = )[^;\n]*;`)
	projectLine     = regexp.MustCompile(`(?m)^(\s*CURRENT_PROJECT_VERSION = )[^;\n]*;`)
	bundleLine      = regexp.MustCompile(`(?m)^\s*PRODUCT_BUNDLE_IDENTIFIER = ([^;\n]*);`)
)

// maxBuild is the largest versionCode Google Play accepts.
const maxBuild = 2_100_000_000

// The adaptive icon draws the full icon in the 72 dp viewport of its 108 dp layer, so a
// launcher's mask never cuts into the maskable safe circle.
const adaptiveIcon = `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground>
        <inset android:drawable="@mipmap/ic_launcher_foreground" android:inset="16.667%"/>
    </foreground>
</adaptive-icon>
`

// mobileOptions is a parsed goje mobile command line.
type mobileOptions struct {
	options
	build     int
	platforms []string
}

// mobile builds the web bundle and generates or syncs the Capacitor projects in DIR/mobile.
func mobile(ctx context.Context, args []string, log io.Writer) error {
	opts, err := parseMobile(args)
	if err != nil {
		return err
	}
	g, err := resolve(ctx, opts.options)
	if err != nil {
		return err
	}
	dir := filepath.Join(g.out, "mobile")
	// Check existing projects first, so a mismatched ID leaves every file untouched.
	for _, platform := range opts.platforms {
		if checkErr := g.checkProjectID(filepath.Join(dir, platform)); checkErr != nil {
			return checkErr
		}
	}
	if buildErr := g.buildWeb(ctx); buildErr != nil {
		return buildErr
	}
	config, err := json.MarshalIndent(map[string]string{
		"appId": g.id, "appName": g.name, "webDir": "../web",
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capacitor.config.json: %w", err)
	}
	// Goje owns these files and replaces them on every run, which also removes any plugin.
	if writeErr := writeFiles(dir, map[string][]byte{
		"package.json": mobilePackage, "package-lock.json": mobileLock,
		"capacitor.config.json": append(config, '\n'),
	}); writeErr != nil {
		return writeErr
	}
	install := []string{"ci", "--ignore-scripts", "--no-audit", "--no-fund"}
	if runErr := command(ctx, dir, log, "npm", install...); runErr != nil {
		return runErr
	}
	for _, platform := range opts.platforms {
		if _, statErr := os.Stat(filepath.Join(dir, platform)); errors.Is(statErr, fs.ErrNotExist) {
			add := []string{"--no-install", "cap", "add", platform}
			if platform == "ios" {
				add = append(add, "--packagemanager", "SPM")
			}
			if runErr := command(ctx, dir, log, "npx", add...); runErr != nil {
				return runErr
			}
		} else if statErr != nil {
			return fmt.Errorf("check the %s project: %w", platform, statErr)
		}
		sync := []string{"--no-install", "cap", "sync", platform}
		if runErr := command(ctx, dir, log, "npx", sync...); runErr != nil {
			return runErr
		}
		if platform == "android" {
			err = g.syncAndroid(filepath.Join(dir, "android"), opts.build)
		} else {
			err = g.syncIOS(filepath.Join(dir, "ios"), opts.build)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Each project file sets the application ID on lines this pattern matches.
var projectIDs = map[string]struct {
	file    []string
	pattern *regexp.Regexp
}{
	"android": {[]string{"app", "build.gradle"}, applicationID},
	"ios":     {[]string{"App", "App.xcodeproj", "project.pbxproj"}, bundleLine},
}

// checkProjectID accepts a missing project or one whose every application ID is g.id.
func (g *game) checkProjectID(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	ids := projectIDs[filepath.Base(dir)]
	file := filepath.Join(append([]string{dir}, ids.file...)...)
	data, err := os.ReadFile(file) // #nosec G304 -- Inside the chosen output directory.
	if err != nil {
		return fmt.Errorf("read the %s project: %w", filepath.Base(dir), err)
	}
	found := ids.pattern.FindAllSubmatch(data, -1)
	if len(found) == 0 {
		return fmt.Errorf("%s sets no application ID", file)
	}
	for _, id := range found {
		if string(id[1]) != g.id {
			return fmt.Errorf("%s sets the application ID %s, not %s; use that ID with -id, "+
				"or remove %s to generate a new project", file, id[1], g.id, dir)
		}
	}
	return nil
}

func parseMobile(args []string) (mobileOptions, error) {
	opts := mobileOptions{command: "mobile", web: true}
	set := flag.NewFlagSet("goje mobile", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&opts.out, "o", "dist", "output directory")
	set.StringVar(&opts.name, "name", "", "game name; defaults to the package's last path element")
	set.StringVar(&opts.version, "version", "0.0.0", "game version")
	set.StringVar(&opts.id, "id", "", "Android application ID and iOS bundle identifier")
	set.IntVar(&opts.build, "build", 0, "native build number, from 1 to 2100000000")
	set.StringVar(&opts.tags, "tags", "", "comma-separated Go build tags")
	set.StringVar(&opts.icon, "icon", "", "square 512 × 512 PNG icon")
	platform := set.String("platform", "all", "android, ios, or all")
	if err := set.Parse(args); err != nil {
		return opts, fmt.Errorf("%w: %w", errUsage, err)
	}
	switch set.NArg() {
	case 0:
		opts.pkg = "."
	case 1:
		opts.pkg = set.Arg(0)
	default:
		return opts, fmt.Errorf("%w: expected at most one package, got %d", errUsage, set.NArg())
	}
	switch *platform {
	case "android", "ios":
		opts.platforms = []string{*platform}
	case "all":
		opts.platforms = []string{"android", "ios"}
	default:
		return opts, fmt.Errorf("%w: -platform %q must be android, ios, or all", errUsage, *platform)
	}
	switch {
	case opts.out == "":
		return opts, fmt.Errorf("%w: -o must not be empty", errUsage)
	case opts.name != "" && !namePattern.MatchString(opts.name):
		return opts, fmt.Errorf("%w: -name %q must match [a-z][a-z0-9-]* and have at most 32 bytes",
			errUsage, opts.name)
	case !versionPattern.MatchString(opts.version):
		return opts, fmt.Errorf("%w: -version %q must look like 1.2.3 or 1.2.3-beta.1",
			errUsage, opts.version)
	case opts.build < 1 || opts.build > maxBuild:
		return opts, fmt.Errorf("%w: -build must be set to a number from 1 to %d", errUsage, maxBuild)
	case strings.ContainsAny(opts.tags, " \t\n"):
		return opts, fmt.Errorf("%w: -tags %q must be comma-separated without spaces",
			errUsage, opts.tags)
	}
	return opts, checkAppID(opts.id)
}

// checkAppID accepts an ID that is valid on Android, as a Java package, and on iOS.
func checkAppID(id string) error {
	if !appIDPattern.MatchString(id) {
		return fmt.Errorf("%w: -id %q must be set to two or more period-separated parts, "+
			"each a letter followed by letters and digits", errUsage, id)
	}
	for part := range strings.SplitSeq(id, ".") {
		if slices.Contains(javaKeywords, part) {
			return fmt.Errorf("%w: -id %q must not contain the Java keyword %q", errUsage, id, part)
		}
	}
	return nil
}

// command runs a Node tool in dir with its output on the log.
func command(ctx context.Context, dir string, log io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- Fixed Node commands.
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, log, log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// syncAndroid sets the Goje-owned fields of the Android project: the version name, the
// version code, and the launcher icon.
func (g *game) syncAndroid(dir string, build int) error {
	gradle := filepath.Join(dir, "app", "build.gradle")
	data, err := os.ReadFile(gradle) // #nosec G304 -- Inside the chosen output directory.
	if err != nil {
		return fmt.Errorf("read the Android build file: %w", err)
	}
	data, err = replaceOne(gradle, data, versionCodeLine, strconv.Itoa(build))
	if err != nil {
		return err
	}
	if data, err = replaceOne(gradle, data, versionNameLine, strconv.Quote(g.version)); err != nil {
		return err
	}
	if writeErr := writeFiles(
		filepath.Dir(gradle),
		map[string][]byte{"build.gradle": data},
	); writeErr != nil {
		return writeErr
	}
	// One high-density copy serves every density; the launcher scales it down.
	res := filepath.Join(dir, "app", "src", "main", "res")
	densities, err := filepath.Glob(filepath.Join(res, "mipmap-*dpi"))
	if err != nil {
		return fmt.Errorf("list launcher icons: %w", err)
	}
	for _, density := range densities {
		for _, file := range []string{"ic_launcher.png", "ic_launcher_round.png", "ic_launcher_foreground.png"} {
			// #nosec G703 -- Inside the chosen output directory.
			removeErr := os.Remove(filepath.Join(density, file))
			if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				return fmt.Errorf("remove a launcher icon: %w", removeErr)
			}
		}
	}
	icons := map[string][]byte{
		"ic_launcher.png": g.png, "ic_launcher_round.png": g.png, "ic_launcher_foreground.png": g.png,
	}
	if writeErr := writeFiles(filepath.Join(res, "mipmap-xxxhdpi"), icons); writeErr != nil {
		return writeErr
	}
	return writeFiles(filepath.Join(res, "mipmap-anydpi-v26"), map[string][]byte{
		"ic_launcher.xml": []byte(adaptiveIcon), "ic_launcher_round.xml": []byte(adaptiveIcon),
	})
}

// syncIOS sets the Goje-owned fields of the iOS project: the marketing version, the
// build number, and the App Store icon.
func (g *game) syncIOS(dir string, build int) error {
	pbx := filepath.Join(dir, "App", "App.xcodeproj", "project.pbxproj")
	data, err := os.ReadFile(pbx) // #nosec G304 -- Inside the chosen output directory.
	if err != nil {
		return fmt.Errorf("read the Xcode project: %w", err)
	}
	if !marketingLine.Match(data) || !projectLine.Match(data) {
		return fmt.Errorf("%s must set MARKETING_VERSION and CURRENT_PROJECT_VERSION", pbx)
	}
	// Apple accepts only the numeric core as the marketing version.
	core, _, _ := strings.Cut(g.version, "-")
	data = marketingLine.ReplaceAll(data, []byte("${1}"+core+";"))
	data = projectLine.ReplaceAll(data, []byte("${1}"+strconv.Itoa(build)+";"))
	if writeErr := writeFiles(
		filepath.Dir(pbx),
		map[string][]byte{"project.pbxproj": data},
	); writeErr != nil {
		return writeErr
	}
	icon, err := appStoreIcon(g.png)
	if err != nil {
		return err
	}
	return writeFiles(filepath.Join(dir, "App", "App", "Assets.xcassets", "AppIcon.appiconset"),
		map[string][]byte{"AppIcon-512@2x.png": icon})
}

// replaceOne sets the value of the one line that pattern matches.
func replaceOne(file string, data []byte, pattern *regexp.Regexp, value string) ([]byte, error) {
	if n := len(pattern.FindAllIndex(data, -1)); n != 1 {
		return nil, fmt.Errorf("%s must have exactly one line matching %s, not %d", file, pattern, n)
	}
	return pattern.ReplaceAll(data, []byte("${1}"+value)), nil
}

// appStoreIcon doubles each pixel of the 512 × 512 icon, which keeps pixel art sharp, and
// fills transparency with the page background, because the App Store rejects an alpha channel.
func appStoreIcon(data []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}
	out := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	origin := src.Bounds().Min
	for y := range 512 {
		for x := range 512 {
			r, g, b, a := src.At(origin.X+x, origin.Y+y).RGBA()
			// RGBA returns premultiplied components. Fill the uncovered fraction
			// with the page background before copying the pixel to a 2×2 block.
			gap := 0xffff - a
			p := color.RGBA{
				R: uint8((r + 0x1414*gap/0xffff) >> 8), // #nosec G115 -- Composite fits in 16 bits.
				G: uint8((g + 0x1c1c*gap/0xffff) >> 8), // #nosec G115 -- Composite fits in 16 bits.
				B: uint8((b + 0x2929*gap/0xffff) >> 8), // #nosec G115 -- Composite fits in 16 bits.
				A: 0xff,
			}
			out.SetRGBA(2*x, 2*y, p)
			out.SetRGBA(2*x+1, 2*y, p)
			out.SetRGBA(2*x, 2*y+1, p)
			out.SetRGBA(2*x+1, 2*y+1, p)
		}
	}
	var encoded bytes.Buffer
	// The encoder writes an opaque image without an alpha channel.
	if err = png.Encode(&encoded, out); err != nil {
		return nil, fmt.Errorf("encode the App Store icon: %w", err)
	}
	return encoded.Bytes(), nil
}

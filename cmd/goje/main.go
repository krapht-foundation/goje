// Command goje builds a Goje game for the web and the host desktop and packages
// release archives, optionally signed. The commands and their outputs are
// specified in specs/build-m15.md and specs/update-m17.md.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
)

const usageText = `usage: goje build|package [-o DIR] [-name NAME] [-version V] [-id BUNDLE_ID]
	[-tags TAGS] [-icon PNG] [-target web|desktop|all] [-sign KEYFILE] [PACKAGE]
       goje keygen [-o PREFIX]
`

var (
	errUsage = errors.New("invalid arguments")
	// Names become file names, bundle names, and archive names, so they stay portable.
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// Versions are a semantic version core with an optional pre-release suffix.
	versionPattern = regexp.MustCompile(
		`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(-[0-9A-Za-z.-]{1,32})?$`,
	)
	// Apple bundle identifiers allow letters, digits, hyphens, and periods.
	bundlePattern = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
)

// options holds the parsed command line. An empty name or bundle ID is derived later.
type options struct {
	command, out, name, version, id, tags, icon, sign, pkg string
	web, desktop                                           bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stderr)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "goje: %v\n", err)
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usageText)
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, log io.Writer) error {
	if len(args) > 0 && args[0] == "keygen" {
		return keygen(args[1:])
	}
	opts, err := parse(args, runtime.GOOS, log)
	if err != nil {
		return err
	}
	var key ed25519.PrivateKey
	if opts.sign != "" {
		// Read the key before building, so a bad key fails fast.
		if key, err = readPrivateKey(opts.sign); err != nil {
			return err
		}
	}
	game, err := resolve(ctx, opts)
	if err != nil {
		return err
	}
	err = game.build(ctx)
	if err != nil {
		return err
	}
	if opts.command != "package" {
		return nil
	}
	modTime, err := sourceDate(os.Getenv("SOURCE_DATE_EPOCH"))
	if err != nil {
		return err
	}
	return pack(opts.out, game.name, opts.version, game.platform, modTime, key)
}

// parse reads and validates the command line. goos is the host system, which
// decides whether a desktop build is possible.
func parse(args []string, goos string, log io.Writer) (options, error) {
	if len(args) == 0 || (args[0] != "build" && args[0] != "package") {
		return options{}, fmt.Errorf(
			"%w: the first argument must be build, package, or keygen", errUsage)
	}
	opts := options{command: args[0]}
	set := flag.NewFlagSet("goje "+args[0], flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&opts.out, "o", "dist", "output directory")
	set.StringVar(&opts.name, "name", "", "game name; defaults to the package's last path element")
	set.StringVar(&opts.version, "version", "0.0.0", "game version")
	set.StringVar(&opts.id, "id", "", "macOS bundle identifier; defaults to com.example.NAME")
	set.StringVar(&opts.tags, "tags", "", "comma-separated Go build tags")
	set.StringVar(&opts.icon, "icon", "", "square 512 × 512 PNG icon")
	target := set.String("target", "all", "web, desktop, or all")
	set.StringVar(&opts.sign, "sign", "", "PEM Ed25519 private key that signs the release")
	if err := set.Parse(args[1:]); err != nil {
		return options{}, fmt.Errorf("%w: %w", errUsage, err)
	}
	switch set.NArg() {
	case 0:
		opts.pkg = "."
	case 1:
		opts.pkg = set.Arg(0)
	default:
		return options{}, fmt.Errorf("%w: expected at most one package, got %d", errUsage, set.NArg())
	}
	switch {
	case opts.out == "":
		return options{}, fmt.Errorf("%w: -o must not be empty", errUsage)
	case opts.name != "" && !namePattern.MatchString(opts.name):
		return options{}, fmt.Errorf(
			"%w: -name %q must match [a-z][a-z0-9-]* and have at most 32 bytes",
			errUsage,
			opts.name,
		)
	case !versionPattern.MatchString(opts.version):
		return options{}, fmt.Errorf("%w: -version %q must look like 1.2.3 or 1.2.3-beta.1",
			errUsage, opts.version)
	case opts.id != "" && !bundlePattern.MatchString(opts.id):
		return options{}, fmt.Errorf(
			"%w: -id %q must be a reverse-DNS name of letters, digits, hyphens, and periods",
			errUsage, opts.id)
	case strings.ContainsAny(opts.tags, " \t\n"):
		return options{}, fmt.Errorf("%w: -tags %q must be comma-separated without spaces",
			errUsage, opts.tags)
	case opts.sign != "" && opts.command != "package":
		return options{}, fmt.Errorf("%w: -sign applies only to package", errUsage)
	}
	desktopErr := desktopSupported(goos)
	switch *target {
	case "web":
		opts.web = true
	case "desktop":
		if desktopErr != nil {
			return options{}, desktopErr
		}
		opts.desktop = true
	case "all":
		opts.web, opts.desktop = true, desktopErr == nil
		if desktopErr == nil {
			break
		}
		if _, err := fmt.Fprintf(log, "goje: building only for the web: %v\n", desktopErr); err != nil {
			return options{}, fmt.Errorf("write note: %w", err)
		}
	default:
		return options{}, fmt.Errorf("%w: -target %q must be web, desktop, or all", errUsage, *target)
	}
	return opts, nil
}

// desktopSupported reports why the host cannot build a desktop executable.
func desktopSupported(goos string) error {
	if goos == "darwin" || goos == "linux" || goos == "windows" {
		return nil
	}
	return fmt.Errorf("desktop builds use the cgo desktop host, which supports only darwin, "+
		"linux, and windows, and run only for the host system, which is %s; use -target web", goos)
}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/krapht-foundation/goje/update"
)

// platformPattern is a desktop GOOS-GOARCH pair as build.go names it.
var platformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)

// updateClient is the HTTP client of goje update; nil means http.DefaultClient.
// Tests replace it to trust their HTTPS server.
var updateClient *http.Client

// manifest signs one update.json over the combined archives of a release, as
// specs/update-m18.md describes. The archives must be exactly NAME-VERSION-web.zip
// and one desktop archive per -platforms entry, the key must match the
// independently configured public key, and the output file must not exist.
func manifest(args []string) error {
	set := flag.NewFlagSet("goje manifest", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	name := set.String("name", "", "game name")
	version := set.String("version", "", "release version")
	sign := set.String("sign", "", "PEM Ed25519 private key")
	pub := set.String("pub", "", "PEM public key the signing key must match")
	platforms := set.String("platforms", "", "comma-separated desktop platforms, like linux-amd64")
	out := set.String("o", "update.json", "output file, which must not exist")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	switch {
	case !namePattern.MatchString(*name):
		return fmt.Errorf("%w: -name %q must match [a-z][a-z0-9-]*", errUsage, *name)
	case !versionPattern.MatchString(*version):
		return fmt.Errorf("%w: -version %q must look like 1.2.3", errUsage, *version)
	case *sign == "" || *pub == "" || *out == "":
		return fmt.Errorf("%w: manifest needs -sign, -pub, and a non-empty -o", errUsage)
	case set.NArg() == 0:
		return fmt.Errorf("%w: manifest needs the release archives", errUsage)
	}
	prefix := *name + "-" + *version + "-"
	want := []string{prefix + "web.zip"}
	if *platforms != "" {
		for platform := range strings.SplitSeq(*platforms, ",") {
			if !platformPattern.MatchString(platform) {
				return fmt.Errorf("%w: platform %q must look like linux-amd64", errUsage, platform)
			}
			ext := ".tar.gz"
			if strings.HasPrefix(platform, "windows-") {
				ext = ".zip"
			}
			want = append(want, prefix+platform+ext)
		}
	}
	slices.Sort(want)
	if len(slices.Compact(slices.Clone(want))) != len(want) {
		return fmt.Errorf("%w: -platforms %q repeats a platform", errUsage, *platforms)
	}
	key, err := readPrivateKey(*sign)
	if err != nil {
		return err
	}
	pubData, err := os.ReadFile(*pub) // #nosec G304 -- The maintainer chose the key file.
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	public, err := update.ParsePublicKey(pubData)
	if err != nil {
		return err
	}
	if !public.Equal(key.Public()) {
		return fmt.Errorf("signing key %s does not match public key %s", *sign, *pub)
	}
	files := make([]update.File, 0, set.NArg())
	for _, archive := range set.Args() {
		file, describeErr := describe(archive)
		if describeErr != nil {
			return describeErr
		}
		files = append(files, file)
	}
	slices.SortFunc(files, func(a, b update.File) int { return strings.Compare(a.Name, b.Name) })
	got := make([]string, len(files))
	for i, file := range files {
		got[i] = file.Name
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("archives %q are not exactly %q", got, want)
	}
	data, err := update.Sign(update.Manifest{Name: *name, Version: *version, Files: files}, key)
	if err != nil {
		return err
	}
	if _, err := update.Verify(data, public); err != nil {
		return err
	}
	return createFile(*out, data)
}

// createFile writes data to a new file and removes it if writing fails.
func createFile(name string, data []byte) error {
	// #nosec G302 G304 -- The maintainer chose the path, and update.json is public.
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	_, err = file.Write(data)
	if err = errors.Join(err, file.Close()); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", name, err), os.Remove(name))
	}
	return nil
}

// checkUpdate verifies the manifest at a fixed HTTPS channel URL and, with -o,
// downloads one archive from its versioned URL into DIR. It fails unless the
// manifest is signed by -key, names -name, and is newer than -current. It never
// installs or runs anything and never replaces a file.
func checkUpdate(ctx context.Context, args []string, log io.Writer) error {
	set := flag.NewFlagSet("goje update", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	manifestURL := set.String("manifest", "", "fixed HTTPS URL of the channel's update.json")
	artifacts := set.String("artifacts", "", "HTTPS archive URL with {version} and {file}")
	keyFile := set.String("key", "", "PEM public key of the channel")
	name := set.String("name", "", "game name")
	current := set.String("current", "", "installed version")
	file := set.String("file", "", "archive to download")
	out := set.String("o", "", "existing directory for the verified archive")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	switch {
	case set.NArg() != 0:
		return fmt.Errorf("%w: update takes no arguments", errUsage)
	case *manifestURL == "" || *keyFile == "" || *name == "" || *current == "":
		return fmt.Errorf("%w: update needs -manifest, -key, -name, and -current", errUsage)
	case (*file == "") != (*out == "") || (*out != "" && *artifacts == ""):
		return fmt.Errorf("%w: a download needs -file, -o, and -artifacts together", errUsage)
	}
	keyData, err := os.ReadFile(*keyFile) // #nosec G304 -- The player chose the key file.
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	key, err := update.ParsePublicKey(keyData)
	if err != nil {
		return err
	}
	m, newer, err := update.Check(ctx, updateClient, *manifestURL, key, *name, *current)
	if err != nil {
		return err
	}
	if !newer {
		return fmt.Errorf("signed version %s is not newer than %s", m.Version, *current)
	}
	if _, err = fmt.Fprintf(log, "verified %s %s\n", m.Name, m.Version); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if *out == "" {
		for _, f := range m.Files {
			if _, err = fmt.Fprintf(log, "%s  %d  %s\n", f.SHA256, f.Size, f.Name); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
		}
		return nil
	}
	fileURL, f, err := update.FileURL(*artifacts, m, *file)
	if err != nil {
		return err
	}
	path, err := update.Save(ctx, updateClient, fileURL, f, filepath.Clean(*out))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(log, "saved %s\n", path); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

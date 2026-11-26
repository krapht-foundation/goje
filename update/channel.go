package update

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Check fetches the manifest at manifestURL over HTTPS, verifies it with key,
// and requires the game name. It reports whether the signed version is newer
// than current; an equal or older manifest is returned with false, never as
// an update. specs/update-m18.md describes the hosted channel. A nil client
// means http.DefaultClient.
func Check(
	ctx context.Context,
	client *http.Client,
	manifestURL string,
	key ed25519.PublicKey,
	name, current string,
) (m Manifest, newer bool, err error) {
	if urlErr := requireHTTPS(manifestURL); urlErr != nil {
		return Manifest{}, false, urlErr
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, http.NoBody)
	if err != nil {
		return Manifest{}, false, fmt.Errorf("update: fetch manifest: %w", err)
	}
	response, err := httpsOnly(client).Do(request)
	if err != nil {
		return Manifest{}, false, fmt.Errorf("update: fetch manifest: %w", err)
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return Manifest{}, false, fmt.Errorf("update: fetch manifest: HTTP %s", response.Status)
	}
	// One byte over the limit is enough for Verify to reject it.
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxManifest+1))
	if err != nil {
		return Manifest{}, false, fmt.Errorf("update: fetch manifest: %w", err)
	}
	if m, err = Verify(data, key); err != nil {
		return Manifest{}, false, err
	}
	if m.Name != name {
		return Manifest{}, false, fmt.Errorf("update: manifest is for %q, not %q", m.Name, name)
	}
	newer, err = Newer(m, current)
	if err != nil {
		return Manifest{}, false, err
	}
	return m, newer, nil
}

// FileURL finds the file called name in m and returns its download URL: pattern
// with {version} replaced by m's signed version and {file} by the file name.
// The pattern must be an HTTPS URL containing both, so every version has its
// own artifact URL and a moving channel URL is never used for an archive.
func FileURL(pattern string, m Manifest, name string) (string, File, error) {
	if err := requireHTTPS(pattern); err != nil {
		return "", File{}, err
	}
	if strings.Count(pattern, "{version}") != 1 || strings.Count(pattern, "{file}") != 1 {
		return "", File{}, fmt.Errorf(
			"update: artifact pattern %q must contain {version} and {file} once each", pattern)
	}
	for _, f := range m.Files {
		if f.Name == name {
			// Validated versions and file names need no escaping in a URL path.
			return strings.NewReplacer("{version}", m.Version, "{file}", f.Name).Replace(pattern), f, nil
		}
	}
	return "", File{}, fmt.Errorf("update: %s %s has no file %q", m.Name, m.Version, name)
}

// Save downloads file from fileURL over HTTPS into dir as file.Name. The bytes
// go to a temporary file in dir that is published only after its size and
// digest verify, by a hard link that fails if the name exists, so Save never
// replaces a file and leaves no partial or unverified file behind on error.
// It returns the published path. A nil client means http.DefaultClient.
func Save(
	ctx context.Context, client *http.Client, fileURL string, file File, dir string,
) (path string, err error) {
	if !fileNamePattern.MatchString(file.Name) {
		return "", fmt.Errorf("update: file name %q is not a plain base name", file.Name)
	}
	if urlErr := requireHTTPS(fileURL); urlErr != nil {
		return "", urlErr
	}
	path = filepath.Join(dir, file.Name)
	// Fail before downloading; the link below still refuses a file that appears later.
	if _, statErr := os.Lstat(path); statErr == nil {
		return "", fmt.Errorf("update: %s already exists", path)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("update: %w", statErr)
	}
	temp, err := os.CreateTemp(dir, "."+file.Name+".*.part")
	if err != nil {
		return "", fmt.Errorf("update: %w", err)
	}
	defer func() {
		// The temporary name never holds a verified file once Save returns.
		err = errors.Join(err, os.Remove(temp.Name()))
		if err != nil {
			path = ""
		}
	}()
	err = Download(ctx, httpsOnly(client), fileURL, file, temp)
	if err == nil {
		err = temp.Sync()
	}
	err = errors.Join(err, temp.Close())
	if err != nil {
		return "", err
	}
	if err := os.Link(temp.Name(), path); err != nil {
		return "", fmt.Errorf("update: publish %s: %w", path, err)
	}
	return path, nil
}

func requireHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("update: %q is not an https URL", raw)
	}
	return nil
}

// httpsOnly returns a copy of client that refuses a redirect to another scheme.
func httpsOnly(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	c := *client
	next := client.CheckRedirect
	c.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" {
			return fmt.Errorf("update: redirect to %s is not https", request.URL)
		}
		if next != nil {
			return next(request, via)
		}
		if len(via) >= 10 {
			return errors.New("update: stopped after 10 redirects")
		}
		return nil
	}
	return &c
}

// Package update verifies a signed release manifest written by `goje package
// -sign`, tells whether it names a newer version, and downloads its files while
// checking their size and SHA-256 digest. specs/update-m17.md specifies the
// manifest, the signed message, and the rules this package enforces.
package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const (
	// Format names the manifest version and separates its signed message from
	// any other message signed with the same key.
	Format = "goje-update-v1"
	// MaxManifest is the largest manifest Verify reads, in bytes.
	MaxManifest = 64 << 10
	// MaxFiles is the most files a manifest lists.
	MaxFiles = 64
	// MaxFileSize is the largest file size a manifest may state, 4 GiB.
	MaxFileSize = 4 << 30
)

var (
	// The same rules as cmd/goje's -name and -version flags.
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	versionPattern = regexp.MustCompile(
		`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(-[0-9A-Za-z.-]{1,32})?$`,
	)
	// Base names only: no separator, and a leading letter or digit excludes "." and "..".
	fileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// File is one released file: a base name relative to the manifest's URL, its
// exact size in bytes, and the lowercase hexadecimal SHA-256 digest.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is the signed content of update.json. Files are sorted by name.
type Manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// signed is the message the signature covers; its encoding/json output is canonical
// because every field is validated to need no escaping.
type signed struct {
	Format string `json:"format"`
	Manifest
}

type document struct {
	signed
	Signature string `json:"signature"`
}

// Sign returns the update.json bytes for m. It rejects a manifest that Verify
// would reject.
func Sign(m Manifest, key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("update: private key has %d bytes, want %d",
			len(key), ed25519.PrivateKeySize)
	}
	if err := check(m); err != nil {
		return nil, err
	}
	message, err := json.Marshal(signed{Format, m})
	if err != nil {
		return nil, fmt.Errorf("update: encode manifest: %w", err)
	}
	return encode(document{
		signed{Format, m}, base64.StdEncoding.EncodeToString(ed25519.Sign(key, message)),
	})
}

func encode(d document) ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("update: encode manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// Verify returns the manifest in data if data is exactly what Sign writes for
// it and the signature verifies with key. Check the returned Name before
// trusting the manifest, because one key may sign several games.
func Verify(data []byte, key ed25519.PublicKey) (Manifest, error) {
	if len(key) != ed25519.PublicKeySize {
		return Manifest{}, fmt.Errorf("update: public key has %d bytes, want %d",
			len(key), ed25519.PublicKeySize)
	}
	if len(data) > MaxManifest {
		return Manifest{}, fmt.Errorf("update: manifest has %d bytes, more than %d",
			len(data), MaxManifest)
	}
	var d document
	if err := json.Unmarshal(data, &d); err != nil {
		return Manifest{}, fmt.Errorf("update: decode manifest: %w", err)
	}
	if d.Format != Format {
		return Manifest{}, fmt.Errorf("update: format %q, want %q", d.Format, Format)
	}
	if err := check(d.Manifest); err != nil {
		return Manifest{}, err
	}
	// Exact bytes leave no unsigned field, duplicate key, or alternative spelling.
	canonical, err := encode(d)
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Manifest{}, errors.New("update: manifest is not in canonical form")
	}
	signature, err := base64.StdEncoding.DecodeString(d.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Manifest{}, errors.New("update: signature is not 64 bytes of standard base64")
	}
	message, err := json.Marshal(d.signed)
	if err != nil {
		return Manifest{}, fmt.Errorf("update: encode manifest: %w", err)
	}
	if !ed25519.Verify(key, message, signature) {
		return Manifest{}, errors.New("update: signature does not match the manifest and key")
	}
	return d.Manifest, nil
}

func check(m Manifest) error {
	switch {
	case !namePattern.MatchString(m.Name):
		return fmt.Errorf("update: name %q must match [a-z][a-z0-9-]{0,31}", m.Name)
	case !versionPattern.MatchString(m.Version):
		return fmt.Errorf("update: version %q must look like 1.2.3 or 1.2.3-beta.1", m.Version)
	case len(m.Files) == 0 || len(m.Files) > MaxFiles:
		return fmt.Errorf("update: %d files, want 1 to %d", len(m.Files), MaxFiles)
	}
	for i, f := range m.Files {
		switch {
		case !fileNamePattern.MatchString(f.Name):
			return fmt.Errorf("update: file name %q is not a plain base name", f.Name)
		case i > 0 && f.Name <= m.Files[i-1].Name:
			return fmt.Errorf("update: file %q is not after %q in byte order",
				f.Name, m.Files[i-1].Name)
		case f.Size < 0 || f.Size > MaxFileSize:
			return fmt.Errorf("update: file %q size %d is outside 0 to %d", f.Name, f.Size, MaxFileSize)
		case !digestPattern.MatchString(f.SHA256):
			return fmt.Errorf("update: file %q digest is not 64 lowercase hexadecimal digits", f.Name)
		}
	}
	return nil
}

// Newer reports whether m names a later version than current by semantic
// version precedence. An equal or older manifest is not newer, so replaying an
// old signed manifest cannot roll a game back.
func Newer(m Manifest, current string) (bool, error) {
	if !versionPattern.MatchString(current) {
		return false, fmt.Errorf("update: current version %q must look like 1.2.3", current)
	}
	if !versionPattern.MatchString(m.Version) {
		return false, fmt.Errorf("update: version %q must look like 1.2.3", m.Version)
	}
	return compareVersions(m.Version, current) > 0, nil
}

// compareVersions orders two valid versions as Semantic Versioning 2.0.0 does.
func compareVersions(a, b string) int {
	aCore, aPre, aHasPre := strings.Cut(a, "-")
	bCore, bPre, bHasPre := strings.Cut(b, "-")
	aNums, bNums := strings.Split(aCore, "."), strings.Split(bCore, ".")
	for i := range aNums {
		if c := compareNumbers(aNums[i], bNums[i]); c != 0 {
			return c
		}
	}
	switch {
	case !aHasPre && !bHasPre:
		return 0
	case !aHasPre:
		return 1
	case !bHasPre:
		return -1
	}
	aIDs, bIDs := strings.Split(aPre, "."), strings.Split(bPre, ".")
	for i := 0; i < len(aIDs) && i < len(bIDs); i++ {
		aNum, bNum := isNumber(aIDs[i]), isNumber(bIDs[i])
		var c int
		switch {
		case aNum && bNum:
			c = compareNumbers(aIDs[i], bIDs[i])
		case aNum:
			c = -1
		case bNum:
			c = 1
		default:
			c = strings.Compare(aIDs[i], bIDs[i])
		}
		if c != 0 {
			return c
		}
	}
	return len(aIDs) - len(bIDs)
}

func isNumber(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// compareNumbers orders decimal strings of any length without overflow.
func compareNumbers(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// Download copies the body of a GET of url to w, failing if it is not exactly
// file.Size bytes with digest file.SHA256. It stops reading at file.Size+1
// bytes. On error, w may hold part of an unverified file: discard it. A nil
// client means http.DefaultClient.
func Download(
	ctx context.Context,
	client *http.Client,
	url string,
	file File,
	w io.Writer,
) (err error) {
	if !digestPattern.MatchString(file.SHA256) || file.Size < 0 || file.Size > MaxFileSize {
		return fmt.Errorf("update: file %q has an invalid size or digest", file.Name)
	}
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("update: download %s: %w", file.Name, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("update: download %s: %w", file.Name, err)
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("update: download %s: HTTP %s", file.Name, response.Status)
	}
	if response.ContentLength > file.Size {
		return fmt.Errorf("update: download %s: %d bytes, more than %d",
			file.Name, response.ContentLength, file.Size)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, hash), io.LimitReader(response.Body, file.Size))
	if err != nil {
		return fmt.Errorf("update: download %s: %w", file.Name, err)
	}
	if n < file.Size {
		return fmt.Errorf("update: download %s: %d bytes, want %d", file.Name, n, file.Size)
	}
	extra, endErr := io.CopyN(io.Discard, response.Body, 1)
	if extra != 0 {
		return fmt.Errorf("update: download %s: more than %d bytes", file.Name, file.Size)
	}
	if !errors.Is(endErr, io.EOF) {
		return fmt.Errorf("update: download %s: %w", file.Name, endErr)
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("update: download %s: SHA-256 digest does not match", file.Name)
	}
	return nil
}

// ParsePublicKey reads the PEM public key that `goje keygen` writes.
func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("update: want a PEM PUBLIC KEY block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("update: parse public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("update: public key is %T, not Ed25519", parsed)
	}
	return key, nil
}

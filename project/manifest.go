package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	maxManifestBytes = 4 << 10
	maxPathBytes     = 128
)

type manifest struct {
	level  string
	ground string
}

func decodeManifest(r io.Reader) (manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return manifest{}, fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	if !utf8.Valid(data) {
		return manifest{}, errors.New("manifest must be UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := readDelimiter(decoder, '{'); err != nil {
		return manifest{}, err
	}
	var result manifest
	var seenVersion bool
	// Read only scalar tokens. Unknown fields and nested values cannot become
	// data structures or overwrite an earlier declaration.
	for decoder.More() {
		field, err := decoder.Token()
		if err != nil {
			return manifest{}, fmt.Errorf("read manifest field: %w", err)
		}
		switch field {
		case "version":
			if seenVersion {
				return manifest{}, errors.New(`duplicate field "version"`)
			}
			seenVersion = true
			if err := readVersion(decoder); err != nil {
				return manifest{}, err
			}
		case "level":
			if err := readAssetPath(decoder, "level", ".json", &result.level); err != nil {
				return manifest{}, err
			}
		case "ground":
			if err := readAssetPath(decoder, "ground", ".png", &result.ground); err != nil {
				return manifest{}, err
			}
		default:
			return manifest{}, fmt.Errorf("unknown field %.64q", field)
		}
	}
	if err := readDelimiter(decoder, '}'); err != nil {
		return manifest{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return manifest{}, errors.New("unexpected data after manifest")
	}
	if !seenVersion {
		return manifest{}, errors.New(`missing field "version"`)
	}
	if result.level == "" {
		return manifest{}, errors.New(`missing field "level"`)
	}
	if result.ground == "" {
		return manifest{}, errors.New(`missing field "ground"`)
	}
	return result, nil
}

func readDelimiter(decoder *json.Decoder, want json.Delim) error {
	value, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("expected %q: %w", want, err)
	}
	if value != want {
		return fmt.Errorf("expected %q", want)
	}
	return nil
}

func readVersion(decoder *json.Decoder) error {
	value, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("version: %w", err)
	}
	number, ok := value.(json.Number)
	if !ok || strings.ContainsAny(string(number), ".eE") {
		return errors.New("version must be a JSON integer")
	}
	if number != "1" {
		return fmt.Errorf("unsupported project version %.64s (want 1)", number)
	}
	return nil
}

func readAssetPath(decoder *json.Decoder, field, ext string, dest *string) error {
	if *dest != "" {
		return fmt.Errorf("duplicate field %q", field)
	}
	value, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	name, ok := value.(string)
	if !ok || !validPath(name, ext) {
		return fmt.Errorf(
			"%s must be a portable relative %s path of at most %d bytes",
			field,
			ext,
			maxPathBytes,
		)
	}
	*dest = name
	return nil
}

func validPath(name, ext string) bool {
	stem, ok := strings.CutSuffix(name, ext)
	if !ok || len(name) > maxPathBytes {
		return false
	}
	for component := range strings.SplitSeq(stem, "/") {
		if component == "" || reserved(component) {
			return false
		}
		for i := range len(component) {
			c := component[i]
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || i > 0 && (c == '_' || c == '-') {
				continue
			}
			return false
		}
	}
	return true
}

func reserved(component string) bool {
	switch component {
	case "con", "prn", "aux", "nul":
		return true
	default:
		return len(component) == 4 && component[3] >= '1' && component[3] <= '9' &&
			(component[:3] == "com" || component[:3] == "lpt")
	}
}

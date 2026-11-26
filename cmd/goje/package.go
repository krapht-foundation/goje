package main

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/krapht-foundation/goje/update"
)

// defaultDate is the archive time when SOURCE_DATE_EPOCH is unset.
var defaultDate = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// sourceDate follows https://reproducible-builds.org/specs/source-date-epoch/.
func sourceDate(epoch string) (time.Time, error) {
	if epoch == "" {
		return defaultDate, nil
	}
	seconds, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil || seconds < 0 {
		return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH %q must be a non-negative integer", epoch)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

// pack writes out/release from out/web and, if platform is set, out/desktop/platform.
// Entries are sorted and carry only the given time and fixed modes, so equal
// inputs give equal bytes. A non-nil key also signs the release.
func pack(out, name, version, platform string, modTime time.Time, key ed25519.PrivateKey) error {
	release := filepath.Join(out, "release")
	if err := fresh(release); err != nil {
		return err
	}
	prefix := filepath.Join(release, name+"-"+version)
	archives := []string{prefix + "-web.zip"}
	if err := writeArchive(archives[0], filepath.Join(out, "web"), modTime, writeZip); err != nil {
		return err
	}
	if platform != "" {
		// Windows has zip support built in and no tar convention.
		archive, write := prefix+"-"+platform+".tar.gz", writeTar
		if strings.HasPrefix(platform, "windows-") {
			archive, write = prefix+"-"+platform+".zip", writeZip
		}
		err := writeArchive(archive, filepath.Join(out, "desktop", platform), modTime, write)
		if err != nil {
			return err
		}
		archives = append(archives, archive)
	}
	return writeChecksums(release, name, version, archives, key)
}

// entry is one archived path, relative and slash-separated, with a fixed mode.
type entry struct {
	path, name string
	mode       fs.FileMode // 0o755 | fs.ModeDir, 0o755, or 0o644.
	size       int64
}

// entries lists dir in lexical order. Only directories and regular files may appear.
func entries(dir string) ([]entry, error) {
	var list []entry
	err := filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {
		if err != nil || file == dir {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", file, err)
		}
		name, err := filepath.Rel(dir, file)
		if err != nil {
			return fmt.Errorf("relate %s to %s: %w", file, dir, err)
		}
		e := entry{path: file, name: filepath.ToSlash(name), mode: 0o644, size: info.Size()}
		switch {
		case info.IsDir():
			e.mode, e.size = fs.ModeDir|0o755, 0
		case !info.Mode().IsRegular():
			return fmt.Errorf("%s is not a regular file or directory", file)
		case info.Mode()&0o111 != 0:
			e.mode = 0o755
		}
		list = append(list, e)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	return list, nil
}

func writeArchive(
	archive, dir string, modTime time.Time,
	write func(io.Writer, []entry, time.Time) error,
) (err error) {
	list, err := entries(dir)
	if err != nil {
		return err
	}
	file, err := os.Create(archive) // #nosec G304 -- The path is inside the chosen output directory.
	if err != nil {
		return fmt.Errorf("create %s: %w", archive, err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := write(file, list, modTime); err != nil {
		return fmt.Errorf("write %s: %w", archive, err)
	}
	return nil
}

func writeZip(w io.Writer, list []entry, modTime time.Time) error {
	archive := zip.NewWriter(w)
	archive.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})
	for _, e := range list {
		if e.mode.IsDir() {
			continue // Zip readers create parent directories from file names.
		}
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: modTime}
		header.SetMode(e.mode)
		dest, err := archive.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("add %s: %w", e.name, err)
		}
		if err := copyFile(dest, e.path); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish zip: %w", err)
	}
	return nil
}

func writeTar(w io.Writer, list []entry, modTime time.Time) error {
	// The gzip header keeps its zero time and empty name.
	compressed, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("start gzip: %w", err)
	}
	archive := tar.NewWriter(compressed)
	for _, e := range list {
		header := &tar.Header{
			Typeflag: tar.TypeReg, Name: e.name, Mode: int64(e.mode.Perm()), Size: e.size,
			ModTime: modTime, Format: tar.FormatPAX,
		}
		if e.mode.IsDir() {
			header.Typeflag, header.Name = tar.TypeDir, e.name+"/"
		}
		if err := archive.WriteHeader(header); err != nil {
			return fmt.Errorf("add %s: %w", e.name, err)
		}
		if !e.mode.IsDir() {
			if err := copyFile(archive, e.path); err != nil {
				return err
			}
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish tar: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("finish gzip: %w", err)
	}
	return nil
}

func copyFile(dest io.Writer, path string) (err error) {
	file, err := os.Open(
		path,
	) // #nosec G304 G703 -- The path comes from the developer's build output.
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := io.Copy(dest, file); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	return nil
}

// writeChecksums writes SHA256SUMS in the sha256sum text format, sorted by file
// name. A non-nil key also writes its signature, SHA256SUMS.sig, and the signed
// update.json, which binds the name and version to the same digests.
func writeChecksums(
	release, name, version string, archives []string, key ed25519.PrivateKey,
) error {
	files := make([]update.File, 0, len(archives))
	for _, archive := range archives {
		file, err := describe(archive)
		if err != nil {
			return err
		}
		files = append(files, file)
	}
	slices.SortFunc(files, func(a, b update.File) int { return strings.Compare(a.Name, b.Name) })
	var sums []byte
	for _, file := range files {
		sums = append(sums, file.SHA256+"  "+file.Name+"\n"...)
	}
	out := map[string][]byte{"SHA256SUMS": sums}
	if key != nil {
		manifest, err := update.Sign(update.Manifest{Name: name, Version: version, Files: files}, key)
		if err != nil {
			return fmt.Errorf("sign update.json: %w", err)
		}
		out["SHA256SUMS.sig"], out["update.json"] = ed25519.Sign(key, sums), manifest
	}
	return writeFiles(release, out)
}

// describe returns the manifest entry of an archive: its base name, size, and digest.
func describe(archive string) (update.File, error) {
	hash := sha256.New()
	if err := copyFile(hash, archive); err != nil {
		return update.File{}, err
	}
	info, err := os.Stat(archive)
	if err != nil {
		return update.File{}, fmt.Errorf("stat %s: %w", archive, err)
	}
	return update.File{
		Name: filepath.Base(archive), Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

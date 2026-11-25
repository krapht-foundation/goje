//go:build !(js && wasm)

package platform

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func newStore(game string) (Store, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	store, err := newDirStore(filepath.Join(config, "goje", game))
	if err != nil {
		return nil, err
	}
	return store, nil
}

// dirStore keeps each value in <dir>/<key>.bin. Put writes a temporary file
// in the same directory and renames it over the old one, so a failed or
// interrupted write never leaves a partial value. A crash can leave a
// *.tmp file behind; it is never read.
type dirStore struct{ dir string }

func newDirStore(dir string) (*dirStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return &dirStore{dir: dir}, nil
}

func (s *dirStore) path(key string) string { return filepath.Join(s.dir, key+".bin") }

func (s *dirStore) Put(key string, value []byte) error {
	if err := checkPut(key, value); err != nil {
		return err
	}
	// CreateTemp opens the file with mode 0o600.
	file, err := os.CreateTemp(s.dir, key+".*.tmp")
	if err != nil {
		return fmt.Errorf("store %q: %w", key, err)
	}
	_, err = file.Write(value)
	if err == nil {
		// Flush to disk before the rename so a crash cannot replace the old
		// value with an empty file.
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = os.Rename(file.Name(), s.path(key))
	}
	if err != nil {
		// The old value is untouched; drop the partial temporary file.
		return fmt.Errorf("store %q: %w", key, errors.Join(err, os.Remove(file.Name())))
	}
	return nil
}

func (s *dirStore) Get(key string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	file, err := os.Open(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("store %q: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store %q: %w", key, err)
	}
	// Read one byte past the limit to refuse a file that grew outside the store.
	value, err := io.ReadAll(io.LimitReader(file, MaxStoreValueSize+1))
	if err = errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("store %q: %w", key, err)
	}
	if len(value) > MaxStoreValueSize {
		return nil, fmt.Errorf("store %q: value exceeds %d bytes", key, MaxStoreValueSize)
	}
	return value, nil
}

func (s *dirStore) Delete(key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := os.Remove(s.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store %q: %w", key, err)
	}
	return nil
}

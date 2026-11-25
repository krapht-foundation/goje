package platform

import (
	"errors"
	"fmt"
)

// MaxStoreValueSize is the largest value a Store keeps, in bytes.
const MaxStoreValueSize = 4 << 20

// ErrNotFound reports that a Store has no value for a key. Test for it with
// errors.Is.
var ErrNotFound = errors.New("store key not found")

// Store keeps small values, such as saves and replays, for one game between
// runs. Keys are 1–32 bytes: a lowercase ASCII letter followed by lowercase
// letters, digits, underscores, and hyphens. Values are at most
// MaxStoreValueSize bytes. A failed Put leaves the previous value in place.
// A Store is not safe for concurrent use.
type Store interface {
	// Put replaces the value for key. The store does not retain value.
	Put(key string, value []byte) error
	// Get returns a new copy of the value for key, or an error wrapping
	// ErrNotFound when there is none.
	Get(key string) ([]byte, error)
	// Delete removes the value for key. Deleting a missing key succeeds.
	Delete(key string) error
}

// NewStore opens the store of a game, named as for replay.Meta: 1–32 bytes of
// a lowercase ASCII letter followed by lowercase letters, digits, and
// underscores. On desktop it keeps one file per key in the directory
// goje/<game> under os.UserConfigDir. In a browser it uses local storage under
// the prefix goje/<game>/, with base64 values, and returns an error when local
// storage is unavailable, for example when the user blocks site data. A game
// that gets an error plays without saving.
func NewStore(game string) (Store, error) {
	if !validStoreName(game, false) {
		return nil, fmt.Errorf("open store: game name %q must be 1-32 bytes of [a-z][a-z0-9_]*", game)
	}
	return newStore(game)
}

func checkPut(key string, value []byte) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if len(value) > MaxStoreValueSize {
		return fmt.Errorf("store %q: value of %d bytes exceeds %d", key, len(value), MaxStoreValueSize)
	}
	return nil
}

func checkKey(key string) error {
	if !validStoreName(key, true) {
		return fmt.Errorf("store key %q must be 1-32 bytes of [a-z][a-z0-9_-]*", key)
	}
	return nil
}

func validStoreName(name string, hyphen bool) bool {
	if name == "" || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && (!hyphen || c != '-') {
			return false
		}
	}
	return true
}

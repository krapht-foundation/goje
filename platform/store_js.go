//go:build js && wasm

package platform

import (
	"encoding/base64"
	"fmt"
	"syscall/js"
)

// localStore keeps values in window.localStorage under goje/<game>/<key> as
// standard base64 text. Browsers throw from localStorage when site data is
// blocked and from setItem when the quota is exceeded; setItem then keeps the
// previous value.
type localStore struct {
	storage js.Value
	prefix  string
}

func newStore(game string) (Store, error) {
	var storage js.Value
	err := catch(func() {
		storage = js.Global().Get("localStorage")
		if storage.Type() == js.TypeObject {
			storage.Call("getItem", "goje/")
		}
	})
	if err == nil && storage.Type() != js.TypeObject {
		err = fmt.Errorf("localStorage is %s", storage.Type())
	}
	if err != nil {
		return nil, fmt.Errorf("open store: local storage is unavailable: %w", err)
	}
	return &localStore{storage: storage, prefix: "goje/" + game + "/"}, nil
}

func (s *localStore) Put(key string, value []byte) error {
	if err := checkPut(key, value); err != nil {
		return err
	}
	text := base64.StdEncoding.EncodeToString(value)
	if err := catch(func() { s.storage.Call("setItem", s.prefix+key, text) }); err != nil {
		return fmt.Errorf("store %q: %w", key, err)
	}
	return nil
}

func (s *localStore) Get(key string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	var item js.Value
	if err := catch(func() { item = s.storage.Call("getItem", s.prefix+key) }); err != nil {
		return nil, fmt.Errorf("store %q: %w", key, err)
	}
	if item.Type() != js.TypeString {
		return nil, fmt.Errorf("store %q: %w", key, ErrNotFound)
	}
	value, err := base64.StdEncoding.DecodeString(item.String())
	if err != nil {
		return nil, fmt.Errorf("store %q: %w", key, err)
	}
	if len(value) > MaxStoreValueSize {
		return nil, fmt.Errorf("store %q: value exceeds %d bytes", key, MaxStoreValueSize)
	}
	return value, nil
}

func (s *localStore) Delete(key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := catch(func() { s.storage.Call("removeItem", s.prefix+key) }); err != nil {
		return fmt.Errorf("store %q: %w", key, err)
	}
	return nil
}

// catch runs fn and returns the JavaScript exception it raised, which
// syscall/js reports as a panic with a js.Error value.
func catch(fn func()) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if jsErr, ok := recovered.(js.Error); ok {
				err = jsErr
				return
			}
			err = fmt.Errorf("javascript call failed: %v", recovered)
		}
	}()
	fn()
	return nil
}

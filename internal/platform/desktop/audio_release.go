//go:build !goje_smoke && cgo && (darwin || linux)

package desktop

import "github.com/gen2brain/malgo"

func audioBackends() []malgo.Backend { return platformBackends() }

// The compiler inlines this no-op in regular builds; no hook runs per callback.
func probeAudio([]byte) {}

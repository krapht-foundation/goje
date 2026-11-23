//go:build !goje_bench

package driver

import "time"

// BenchFrameStart is an inlinable no-op outside benchmark builds.
func BenchFrameStart() time.Time { return time.Time{} }

// BenchFrameEnd is an inlinable no-op outside benchmark builds.
func BenchFrameEnd(time.Time) {}

// BenchDropped is an inlinable no-op outside benchmark builds.
func BenchDropped(int64) {}

// BenchKeepActive is false outside benchmark builds.
func BenchKeepActive() bool { return false }

func benchTickStart() time.Time { return time.Time{} }
func benchTickEnd(time.Time)    {}

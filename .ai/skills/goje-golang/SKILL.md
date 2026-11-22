---
name: goje-golang
description: Write or review Goje code for Go 1.27 and newer, including APIs, ownership, concurrency, errors, tests, and desktop or browser builds.
---

# Go 1.27+ development

- Treat the `go.mod` `go` directive as the compatibility floor, not the locally installed toolchain. Prefer the latest patch of a supported release. Check [Go 1.27 release notes](https://go.dev/doc/go1.27) and `go doc` before relying on version-sensitive APIs. Go 1.27 supports type parameters on concrete methods, not interface methods. Use them only when the typed API is clearer.
- Use short, cohesive package APIs. Accept narrow interfaces where used and return concrete types by default. Document what callers can retain, mutate, or use concurrently, especially slices or views backed by engine-owned buffers. Avoid avoidable copies in hot paths and hidden aliasing elsewhere.
- Add meaningful operation context to errors. Wrap with `%w` only when callers should depend on the underlying error; use `errors.Is` or `errors.As` rather than matching messages. Handle close and flush failures when they can lose user data. Reserve panics for programmer invariants, not bad input.
- Give goroutines an owner and a shutdown path. Pass `context.Context` for cancellable operations; do not use it as an option bag or per-frame state. Use single-owner state, channels, or locks as appropriate. Do not assume desktop threading, blocking I/O, or cgo works in browser builds.
- Use `make fmt` for Go formatting and `go test` on affected packages. Use executable examples for public API usage, deterministic fixtures for timing, and `testing/synctest` for compatible concurrent tests rather than wall-clock sleeps. Run `make test-race` on a supported desktop target for synchronization changes. Verify browser-facing functionality in a browser; compilation alone is not runtime evidence.

.DEFAULT_GOAL := help

GO ?= go
NODE ?= node
NPM ?= npm
PYTHON ?= python3
PORT ?= 8080
PLAYWRIGHT_INSTALL_FLAGS ?=
BUILD_DIR := $(CURDIR)/.build
TOOLBIN := $(BUILD_DIR)/tools
WEB_DIR := $(BUILD_DIR)/web
EDITOR_WEB_DIR := $(BUILD_DIR)/leveleditor-web
EDITOR_PORT ?= 8081
PLAYWRIGHT_CACHE := $(BUILD_DIR)/playwright
PLAYGROUND := $(BUILD_DIR)/playground$(if $(filter windows,$(shell $(GO) env GOOS)),.exe)
BENCHMARK := $(BUILD_DIR)/playground-benchmark
BENCHMARK_WEB_DIR := $(BUILD_DIR)/benchmark-web
BENCHMARK_REPEATS ?= 3

include tools.mk

.PHONY: help doctor setup-tools fmt fmt-check lint test test-race vuln docs-check check
help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-z-]*:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doctor: ## Report the effective Go toolchain and module requirement
	@$(GO) list -m -f 'Go module minimum: {{.GoVersion}}'
	@$(GO) version

setup-tools: $(GOLANGCI) $(GOVULNCHECK) ## Install pinned Go developer tools

fmt: $(GOLANGCI) ## Format Go source in place
	"$(GOLANGCI)" fmt ./...

fmt-check: $(GOLANGCI) ## Check Go formatting without rewriting files
	"$(GOLANGCI)" fmt --diff ./...

lint: $(GOLANGCI) ## Run the Go lint policy in .golangci.yml
	"$(GOLANGCI)" run ./...

test: ## Run headless Go tests
	$(GO) test ./...

test-race: ## Run Go tests with the native race detector
	$(GO) test -race ./...

vuln: $(GOVULNCHECK) ## Scan Go dependencies on the current target
	"$(GOVULNCHECK)" ./...

docs-check: ## Check local Markdown links and heading anchors
	$(PYTHON) scripts/check-docs.py

check: fmt-check lint test docs-check ## Run the read-only quality gate

.PHONY: build-desktop run-desktop smoke-desktop build-web serve-web setup-web smoke-web check-example
build-desktop: ## Build the native playground
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -o "$(PLAYGROUND)" ./cmd/playground

run-desktop: build-desktop ## Open the native playground window
	"$(PLAYGROUND)"

smoke-desktop: ## Check all playground fixtures in native windows
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -tags goje_smoke -o "$(BUILD_DIR)/playground-smoke" ./cmd/playground
	$(PYTHON) -c 'import subprocess, sys; subprocess.run([sys.argv[1]], check=True, timeout=20)' "$(BUILD_DIR)/playground-smoke"

build-web: ## Build the playground Wasm and browser files
	mkdir -p "$(WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(WEB_DIR)/playground.wasm" ./cmd/playground
	install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(WEB_DIR)/"

serve-web: build-web ## Serve the playground on loopback (PORT=8080 by default)
	$(NODE) scripts/playground-server.mjs "$(PORT)"

setup-web: ## Install pinned browser smoke dependencies and Chromium
	$(NPM) ci
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NPM) exec -- playwright install --only-shell $(PLAYWRIGHT_INSTALL_FLAGS) chromium

smoke-web: build-web ## Drive the Wasm playground in Chromium and check canvas pixels
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-web.mjs

.PHONY: build-level-editor serve-level-editor smoke-level-editor smoke-level-roundtrip
build-level-editor: ## Build the standalone browser Level Builder
	mkdir -p "$(EDITOR_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(EDITOR_WEB_DIR)/editor.wasm" ./cmd/leveleditor
	install -m 0644 web/leveleditor/index.html web/leveleditor/editor.mjs web/leveleditor/editor.css \
		"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(EDITOR_WEB_DIR)/"

serve-level-editor: build-level-editor ## Serve the Level Builder on loopback (EDITOR_PORT=8081 by default)
	$(NODE) scripts/level-editor-server.mjs "$(EDITOR_PORT)"

smoke-level-editor: build-level-editor ## Verify editing and download in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-level-editor.mjs

smoke-level-roundtrip: build-level-editor ## Play a browser-exported level on desktop and web
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/level-roundtrip.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)"; \
		mkdir -p "$$tmp/playground/levels" "$$tmp/web"; \
		cp cmd/playground/*.go "$$tmp/playground/"; \
		level="$$tmp/playground/levels/courtyard.json"; \
		$(NODE) scripts/smoke-level-editor.mjs "$$level"; \
		$(GO) run ./cmd/levelcheck "$$level"; \
		$(GO) build -tags goje_smoke -o "$$tmp/playground-smoke" "$$tmp/playground"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/web/playground.wasm" "$$tmp/playground"; \
		install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/web/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=20)' "$$tmp/playground-smoke" --shortcut; \
		$(NODE) scripts/smoke-web.mjs "$$tmp/web"

check-example: ## Build the reference playground from a separate module on desktop and web
	@set -eu; tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		cp cmd/playground/main.go cmd/playground/game.go cmd/playground/courtyard.go "$$tmp/"; \
		cp cmd/playground/authored.go "$$tmp/"; \
		cp -R cmd/playground/levels "$$tmp/"; \
		go_version=$$($(GO) list -m -f '{{.GoVersion}}'); \
		cd "$$tmp"; export GOWORK=off; \
		$(GO) mod init example.com/playground; \
		$(GO) mod edit -go="$$go_version" \
			-require=github.com/krapht-foundation/goje@v0.0.0 \
			-replace=github.com/krapht-foundation/goje="$(CURDIR)"; \
		$(GO) mod tidy; \
		$(GO) build .; \
		GOOS=js GOARCH=wasm $(GO) build -o playground.wasm .; \
		echo "External reference builds passed: desktop and Wasm"

.PHONY: benchmark-desktop benchmark-web
BENCHMARK_FIXTURE ?= pickup

benchmark-desktop: ## Measure the native playground with diagnostics off and on
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -tags goje_bench -o "$(BENCHMARK)" ./cmd/playground
	@set -eu; for sprites in 0 2048; do \
		i=0; while [ $$i -lt "$(BENCHMARK_REPEATS)" ]; do \
			if [ $$((i % 2)) -eq 0 ]; then modes='off on'; else modes='on off'; fi; \
			for mode in $$modes; do \
				GOJE_BENCH_MODE=$$mode GOJE_BENCH_SPRITES=$$sprites GOJE_BENCH_FIXTURE="$(BENCHMARK_FIXTURE)" "$(BENCHMARK)"; \
			done; \
			i=$$((i + 1)); \
		done; \
	done

benchmark-web: ## Measure the Chromium playground with diagnostics off and on
	mkdir -p "$(BENCHMARK_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -tags goje_bench -o "$(BENCHMARK_WEB_DIR)/playground.wasm" ./cmd/playground
	install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(BENCHMARK_WEB_DIR)/"
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/benchmark-web.mjs "$(BENCHMARK_WEB_DIR)" "$(BENCHMARK_REPEATS)" "$(BENCHMARK_FIXTURE)"

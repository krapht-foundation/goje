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
TILE_MAKER_WEB_DIR := $(BUILD_DIR)/tilemaker-web
TILE_MAKER_PORT ?= 8082
SPRITE_EDITOR_WEB_DIR := $(BUILD_DIR)/spriteeditor-web
SPRITE_EDITOR_PORT ?= 8083
PIXEL_ART_WEB_DIR := $(BUILD_DIR)/pixelart-web
PIXEL_ART_PORT ?= 8084
SOUND_MAKER_WEB_DIR := $(BUILD_DIR)/soundmaker-web
SOUND_MAKER_PORT ?= 8085
FONT_EDITOR_WEB_DIR := $(BUILD_DIR)/fonteditor-web
FONT_EDITOR_PORT ?= 8086
PLAYWRIGHT_CACHE := $(BUILD_DIR)/playwright
PLAYGROUND := $(BUILD_DIR)/playground$(if $(filter windows,$(shell $(GO) env GOOS)),.exe)
BENCHMARK := $(BUILD_DIR)/playground-benchmark
BENCHMARK_WEB_DIR := $(BUILD_DIR)/benchmark-web
BENCHMARK_REPEATS ?= 3

include tools.mk

.PHONY: help doctor setup-tools fmt fmt-check lint test test-race test-wasm vuln docs-check check
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

test-wasm: ## Run the simulation tests as WebAssembly under Node
	GOOS=js GOARCH=wasm $(GO) test -exec="$$($(GO) env GOROOT)/lib/wasm/go_js_wasm_exec" \
		./engine/fixed ./engine/collide ./internal/collidebench ./examples/skirmish ./internal/skirmishbench ./font ./ui ./engine/replay ./engine/save ./engine ./engine/vision ./engine/lockstep ./engine/matchlog ./netlink/... ./audio ./internal/sound ./internal/soundmaker ./level ./internal/leveleditor ./internal/fonteditor ./cmd/playground

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
	for scenario in --pickup --courtyard --authored --yard --skirmish --outpost; do \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$(BUILD_DIR)/playground-smoke" $$scenario || exit 1; \
	done

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

.PHONY: smoke-placement-editor
smoke-placement-editor: build-level-editor ## Add, change, and delete placements in the Level Builder in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-placement-editor.mjs

.PHONY: smoke-versus-editor
smoke-versus-editor: build-level-editor ## Reauthor the versus level in Chromium and compare the exact export
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/versus-editor.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-versus-editor.mjs "$$tmp/versus.json"

.PHONY: build-tile-maker serve-tile-maker smoke-tile-maker
build-tile-maker: ## Build the standalone browser Tile Maker
	mkdir -p "$(TILE_MAKER_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(TILE_MAKER_WEB_DIR)/tilemaker.wasm" ./cmd/tilemaker
	install -m 0644 web/tilemaker/index.html web/tilemaker/tilemaker.mjs web/tilemaker/tilemaker.css \
		"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(TILE_MAKER_WEB_DIR)/"

serve-tile-maker: build-tile-maker ## Serve the Tile Maker on loopback (TILE_MAKER_PORT=8082 by default)
	$(NODE) scripts/tile-maker-server.mjs "$(TILE_MAKER_PORT)"

smoke-tile-maker: build-tile-maker ## Verify pixel editing, previews, and PNG handoff in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-tile-maker.mjs

.PHONY: build-pixel-art serve-pixel-art smoke-pixel-art
build-pixel-art: ## Build the standalone browser Pixel Art Maker
	mkdir -p "$(PIXEL_ART_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(PIXEL_ART_WEB_DIR)/pixelart.wasm" ./cmd/pixelart
	install -m 0644 web/pixelart/index.html web/pixelart/editor.mjs web/pixelart/editor.css \
		"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(PIXEL_ART_WEB_DIR)/"

serve-pixel-art: build-pixel-art ## Serve the Pixel Art Maker on loopback (PIXEL_ART_PORT=8084 by default)
	$(NODE) scripts/pixel-art-server.mjs "$(PIXEL_ART_PORT)"

smoke-pixel-art: build-pixel-art ## Verify artwork editing and PNG handoff in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-pixel-art.mjs

.PHONY: build-sound-maker serve-sound-maker smoke-sound-maker
build-sound-maker: ## Build the standalone browser Sound Maker
	mkdir -p "$(SOUND_MAKER_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(SOUND_MAKER_WEB_DIR)/soundmaker.wasm" ./cmd/soundmaker
	install -m 0644 web/soundmaker/index.html web/soundmaker/editor.mjs web/soundmaker/editor.css \
		"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(SOUND_MAKER_WEB_DIR)/"

serve-sound-maker: build-sound-maker ## Serve the Sound Maker on loopback (SOUND_MAKER_PORT=8085 by default)
	$(NODE) scripts/sound-maker-server.mjs "$(SOUND_MAKER_PORT)"

smoke-sound-maker: build-sound-maker ## Edit, preview, and export effects and music in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-sound-maker.mjs

.PHONY: build-font-editor serve-font-editor smoke-font-editor
build-font-editor: ## Build the standalone browser Font Editor
	mkdir -p "$(FONT_EDITOR_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(FONT_EDITOR_WEB_DIR)/fonteditor.wasm" ./cmd/fonteditor
	install -m 0644 web/fonteditor/index.html web/fonteditor/editor.mjs web/fonteditor/editor.css \
		"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(FONT_EDITOR_WEB_DIR)/"

serve-font-editor: build-font-editor ## Serve the Font Editor on loopback (FONT_EDITOR_PORT=8086 by default)
	$(NODE) scripts/font-editor-server.mjs "$(FONT_EDITOR_PORT)"

smoke-font-editor: build-font-editor ## Edit a glyph in Chromium and check the export with fontcheck and font.Decode
	GO="$(GO)" PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-font-editor.mjs

.PHONY: build-sprite-editor serve-sprite-editor smoke-sprite-editor smoke-sprite-roundtrip
build-sprite-editor: ## Build the standalone browser Sprite Clip Editor
	mkdir -p "$(SPRITE_EDITOR_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -o "$(SPRITE_EDITOR_WEB_DIR)/spriteeditor.wasm" ./cmd/spriteeditor
	install -m 0644 web/spriteeditor/index.html web/spriteeditor/editor.mjs web/spriteeditor/editor.css \
		"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(SPRITE_EDITOR_WEB_DIR)/"

serve-sprite-editor: build-sprite-editor ## Serve the Sprite Clip Editor on loopback (SPRITE_EDITOR_PORT=8083 by default)
	$(NODE) scripts/sprite-editor-server.mjs "$(SPRITE_EDITOR_PORT)"

smoke-sprite-editor: build-sprite-editor ## Verify clip editing, playback, and JSON handoff in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-sprite-editor.mjs

smoke-sprite-roundtrip: build-pixel-art build-sprite-editor ## Paint, animate, and play both editor downloads on desktop and web
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/sprite-roundtrip.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)"; \
		mkdir -p "$$tmp/courtyard/web" "$$tmp/beacon/web"; \
		$(NODE) scripts/smoke-sprite-editor.mjs "$$tmp"; \
		png="$$tmp/tomato.png"; \
		$(GO) run ./cmd/spritecheck "$$png" "$$tmp/tomato.json"; \
		cp cmd/playground/*.go cmd/playground/goje.json cmd/playground/outpost_music.json "$$tmp/courtyard/"; \
		cp -R cmd/playground/levels cmd/playground/art cmd/playground/sounds "$$tmp/courtyard/"; \
		cp examples/beacon/*.go "$$tmp/beacon/"; \
		cp -R examples/beacon/assets "$$tmp/beacon/"; \
		for assets in "$$tmp/courtyard/art" "$$tmp/beacon/assets"; do \
			cp "$$png" "$$tmp/tomato.json" "$$assets/"; \
		done; \
		$(GO) build -tags goje_smoke -o "$$tmp/courtyard-smoke" "$$tmp/courtyard"; \
		$(GO) build -tags goje_smoke,goje_beacon_smoke -o "$$tmp/beacon-smoke" "$$tmp/beacon"; \
		wasm_exec="$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/courtyard/web/playground.wasm" "$$tmp/courtyard"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/beacon/web/playground.wasm" "$$tmp/beacon"; \
		install -m 0644 web/playground/index.html "$$wasm_exec" "$$tmp/courtyard/web/"; \
		install -m 0644 examples/beacon/web/index.html "$$wasm_exec" "$$tmp/beacon/web/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$$tmp/courtyard-smoke" --sprite; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$$tmp/beacon-smoke" --sprite; \
		$(NODE) scripts/smoke-web.mjs --sprite "$$tmp/courtyard/web"; \
		$(NODE) scripts/smoke-beacon.mjs sprite "$$tmp/beacon/web"

smoke-level-roundtrip: build-level-editor build-tile-maker ## Play both browser exports on desktop and web
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/level-roundtrip.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)"; \
		mkdir -p "$$tmp/playground" "$$tmp/web"; \
		cp cmd/playground/*.go cmd/playground/outpost_music.json "$$tmp/playground/"; \
		cp -R cmd/playground/levels cmd/playground/art cmd/playground/sounds "$$tmp/playground/"; \
		printf '%s\n' '{"version":1,"level":"levels/edited.json","ground":"art/edited.png"}' > "$$tmp/playground/goje.json"; \
		tile="$$tmp/playground/art/edited.png"; \
		$(NODE) scripts/smoke-tile-maker.mjs "$$tile"; \
		$(GO) run ./cmd/tilecheck "$$tile"; \
		level="$$tmp/playground/levels/edited.json"; \
		$(NODE) scripts/smoke-level-editor.mjs "$$level"; \
		$(GO) run ./cmd/levelcheck "$$level"; \
		$(GO) run ./cmd/projectcheck "$$tmp/playground"; \
		$(GO) build -tags goje_smoke -o "$$tmp/playground-smoke" "$$tmp/playground"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/web/playground.wasm" "$$tmp/playground"; \
		install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/web/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$$tmp/playground-smoke" --shortcut; \
		$(NODE) scripts/smoke-web.mjs "$$tmp/web"

.PHONY: smoke-zone-roundtrip
smoke-zone-roundtrip: build-level-editor ## Author the yard's zones in the Level Builder and play them on desktop and web
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/zone-roundtrip.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)"; \
		mkdir -p "$$tmp/playground" "$$tmp/web"; \
		cp cmd/playground/*.go cmd/playground/goje.json cmd/playground/outpost_music.json "$$tmp/playground/"; \
		cp -R cmd/playground/levels cmd/playground/art cmd/playground/sounds "$$tmp/playground/"; \
		level="$$tmp/playground/levels/yard.json"; \
		rm "$$level"; \
		$(NODE) scripts/smoke-zone-editor.mjs "$$level"; \
		$(GO) run ./cmd/levelcheck "$$level"; \
		$(GO) build -tags goje_smoke -o "$$tmp/playground-smoke" "$$tmp/playground"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/web/playground.wasm" "$$tmp/playground"; \
		install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/web/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$$tmp/playground-smoke" --yard; \
		$(NODE) scripts/smoke-web.mjs --yard "$$tmp/web"

.PHONY: smoke-outpost-roundtrip
smoke-outpost-roundtrip: build-level-editor ## Author the outpost's placements and zones in the Level Builder and play them on desktop and web
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/outpost-roundtrip.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)"; \
		mkdir -p "$$tmp/playground" "$$tmp/web"; \
		cp cmd/playground/*.go cmd/playground/goje.json cmd/playground/outpost_music.json "$$tmp/playground/"; \
		cp -R cmd/playground/levels cmd/playground/art cmd/playground/sounds "$$tmp/playground/"; \
		level="$$tmp/playground/levels/outpost.json"; \
		rm "$$level"; \
		$(NODE) scripts/smoke-outpost-editor.mjs "$$level"; \
		$(GO) build -tags goje_smoke -o "$$tmp/playground-smoke" "$$tmp/playground"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/web/playground.wasm" "$$tmp/playground"; \
		install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/web/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$$tmp/playground-smoke" --outpost; \
		$(NODE) scripts/smoke-web.mjs --outpost "$$tmp/web"

.PHONY: smoke-beacon
smoke-beacon: build-level-editor build-tile-maker ## Author and play an independent 2D game on desktop and web
	@set -eu; tmp=$$(mktemp -d "$(BUILD_DIR)/beacon.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GOWORK=off; \
		mkdir -p "$$tmp/assets" "$$tmp/web"; \
		cp examples/beacon/*.go "$$tmp/"; \
		cp examples/beacon/assets/goje.json "$$tmp/assets/"; \
		for asset in tomato.png tomato.json; do \
			cmp "sprite/testdata/$$asset" "cmd/playground/art/$$asset"; \
			cmp "sprite/testdata/$$asset" "examples/beacon/assets/$$asset"; \
		done; \
		cp examples/beacon/assets/tomato.png examples/beacon/assets/tomato.json "$$tmp/assets/"; \
		cp examples/beacon/assets/toggle.wav examples/beacon/assets/music.flac "$$tmp/assets/"; \
		$(NODE) scripts/smoke-beacon.mjs author "$$tmp/assets"; \
		$(GO) run ./cmd/projectcheck "$$tmp/assets"; \
		go_version=$$($(GO) list -m -f '{{.GoVersion}}'); \
		$(GO) build -tags goje_smoke,goje_beacon_smoke -o "$$tmp/beacon-smoke" "$$tmp"; \
		rm "$$tmp/run_smoke.go" "$$tmp/run_bench.go"; \
		cd "$$tmp"; \
		$(GO) mod init example.com/beacon; \
		$(GO) mod edit -go="$$go_version" \
			-require=github.com/krapht-foundation/goje@v0.0.0 \
			-replace=github.com/krapht-foundation/goje="$(CURDIR)"; \
		$(GO) mod tidy; \
		$(GO) build .; \
		GOOS=js GOARCH=wasm $(GO) build -o web/playground.wasm .; \
		install -m 0644 "$(CURDIR)/examples/beacon/web/index.html" \
			"$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" web/; \
		$(PYTHON) -c 'import subprocess; subprocess.run(["./beacon-smoke"], check=True, timeout=20)'; \
		$(NODE) "$(CURDIR)/scripts/smoke-beacon.mjs" play "$$tmp/web"

.PHONY: smoke-beacon-audio
smoke-beacon-audio: ## Verify Beacon PCM output on desktop and in Chromium
	@set -eu; mkdir -p "$(BUILD_DIR)"; \
		tmp=$$(mktemp -d "$(BUILD_DIR)/beacon-audio.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		$(GO) build -tags goje_smoke,goje_beacon_smoke -o "$$tmp/beacon" ./examples/beacon; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/playground.wasm" ./examples/beacon; \
		install -m 0644 examples/beacon/web/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run([sys.argv[1], "--audio"], check=True, timeout=10)' "$$tmp/beacon"; \
		PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-beacon-audio.mjs "$$tmp"

.PHONY: smoke-input
smoke-input: build-web ## Drive the input probe with touch and a fake gamepad in Chromium
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/smoke-input.mjs

.PHONY: smoke-mixer
smoke-mixer: ## Compare overlapping cues and looping music in the Go mixer and the Web Audio graph
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GO="$(GO)" $(NODE) scripts/smoke-mixer.mjs

.PHONY: smoke-goje-build
smoke-goje-build: build-level-editor build-tile-maker ## Build and package Beacon with cmd/goje and play both outputs
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GO="$(GO)" NODE="$(NODE)" PYTHON="$(PYTHON)" scripts/smoke-goje-build.sh

.PHONY: smoke-ws smoke-online benchmark-lockstep
smoke-ws: ## Check the relay and the WebSocket subset against Chromium's own WebSocket
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GO="$(GO)" $(NODE) scripts/smoke-ws.mjs

smoke-online: build-web ## Play co-op and versus, inject a divergence, and verify live watch, rejoin, and replays
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GO="$(GO)" $(NODE) scripts/smoke-online.mjs
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GO="$(GO)" $(NODE) scripts/smoke-online.mjs --fault
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" GO="$(GO)" $(NODE) scripts/smoke-online.mjs --versus --live

benchmark-lockstep: ## Measure one lockstep tick for two players natively and as WebAssembly under Node
	$(GO) test -run '^$$' -bench . -benchmem ./engine/lockstep
	GOOS=js GOARCH=wasm $(GO) test -run '^$$' -bench . -benchmem \
		-exec="$$($(GO) env GOROOT)/lib/wasm/go_js_wasm_exec" ./engine/lockstep

.PHONY: benchmark-mixer
benchmark-mixer: ## Measure sound and music mixing per 512-frame buffer natively and as WebAssembly under Node
	$(GO) test -run '^$$' -bench 'MixerRender|MixerMusic|Queue' -benchmem ./audio ./internal/sound
	GOOS=js GOARCH=wasm $(GO) test -run '^$$' -bench 'MixerRender|MixerMusic|Queue' -benchmem \
		-exec="$$($(GO) env GOROOT)/lib/wasm/go_js_wasm_exec" ./audio ./internal/sound

.PHONY: smoke-sound-roundtrip
smoke-sound-roundtrip: build-sound-maker ## Make a sound in the Sound Maker, then play that download in Beacon on desktop and web
	@set -eu; mkdir -p "$(BUILD_DIR)"; \
		tmp=$$(mktemp -d "$(BUILD_DIR)/sound-roundtrip.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		export PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)"; \
		mkdir -p "$$tmp/beacon/web"; \
		$(NODE) scripts/smoke-sound-maker.mjs "$$tmp/toggle.wav"; \
		if cmp -s "$$tmp/toggle.wav" examples/beacon/assets/toggle.wav; then echo 'the export must differ from the shipped cue' >&2; exit 1; fi; \
		cp examples/beacon/*.go "$$tmp/beacon/"; \
		cp -R examples/beacon/assets "$$tmp/beacon/"; \
		cp "$$tmp/toggle.wav" "$$tmp/beacon/assets/toggle.wav"; \
		$(GO) build -tags goje_smoke,goje_beacon_smoke -o "$$tmp/beacon-smoke" "$$tmp/beacon"; \
		GOOS=js GOARCH=wasm $(GO) build -o "$$tmp/beacon/web/playground.wasm" "$$tmp/beacon"; \
		install -m 0644 examples/beacon/web/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/beacon/web/"; \
		$(PYTHON) -c 'import subprocess, sys; subprocess.run([sys.argv[1], "--audio"], check=True, timeout=10)' "$$tmp/beacon-smoke"; \
		$(NODE) scripts/smoke-beacon-audio.mjs "$$tmp/beacon/web" "$$tmp/toggle.wav"

.PHONY: benchmark-beacon-audio
benchmark-beacon-audio: ## Compare audio off, idle, and burst modes on desktop and in Chromium
	@set -eu; mkdir -p "$(BUILD_DIR)"; \
		tmp=$$(mktemp -d "$(BUILD_DIR)/beacon-audio-bench.XXXXXX"); trap 'rm -rf "$$tmp"' EXIT; \
		$(GO) build -tags goje_bench -o "$$tmp/beacon" ./examples/beacon; \
		GOOS=js GOARCH=wasm $(GO) build -tags goje_bench -o "$$tmp/playground.wasm" ./examples/beacon; \
		install -m 0644 examples/beacon/web/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$$tmp/"; \
		i=0; while [ $$i -lt "$(BENCHMARK_REPEATS)" ]; do \
			if [ $$((i % 2)) -eq 0 ]; then modes='off idle burst'; else modes='burst idle off'; fi; \
			for mode in $$modes; do GOJE_BENCH_MODE=$$mode "$$tmp/beacon"; done; \
			i=$$((i + 1)); \
		done; \
		PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/benchmark-web.mjs "$$tmp" "$(BENCHMARK_REPEATS)" beacon

check-example: ## Build the reference playground from a separate module on desktop and web
	@set -eu; tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		cp cmd/playground/main.go cmd/playground/game.go cmd/playground/courtyard.go "$$tmp/"; \
		cp cmd/playground/authored.go cmd/playground/yard.go cmd/playground/skirmish.go cmd/playground/goje.json "$$tmp/"; \
		cp cmd/playground/outpost.go cmd/playground/outpost_app.go cmd/playground/outpost_session.go "$$tmp/"; \
		cp cmd/playground/outpost_controls.go cmd/playground/outpost_sounds.go cmd/playground/inputprobe.go "$$tmp/"; \
		cp cmd/playground/outpost_online.go cmd/playground/outpost_online_app.go cmd/playground/outpost_match_watch.go "$$tmp/"; \
		cp cmd/playground/outpost_online_config.go cmd/playground/outpost_online_config_js.go "$$tmp/"; \
		cp cmd/playground/outpost_music.json "$$tmp/"; \
		cp -R cmd/playground/levels cmd/playground/art cmd/playground/sounds "$$tmp/"; \
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
BENCHMARK_DENSE := $(if $(filter ground,$(BENCHMARK_FIXTURE)),16384,2048)

benchmark-desktop: ## Compare the selected playground workload on desktop
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -tags goje_bench -o "$(BENCHMARK)" ./cmd/playground
	@set -eu; for sprites in 0 $(BENCHMARK_DENSE); do \
		i=0; while [ $$i -lt "$(BENCHMARK_REPEATS)" ]; do \
			if [ $$((i % 2)) -eq 0 ]; then modes='off on'; else modes='on off'; fi; \
			for mode in $$modes; do \
				GOJE_BENCH_MODE=$$mode GOJE_BENCH_SPRITES=$$sprites GOJE_BENCH_FIXTURE="$(BENCHMARK_FIXTURE)" "$(BENCHMARK)"; \
			done; \
			i=$$((i + 1)); \
		done; \
	done

benchmark-web: ## Compare the selected playground workload in Chromium
	mkdir -p "$(BENCHMARK_WEB_DIR)"
	GOOS=js GOARCH=wasm $(GO) build -tags goje_bench -o "$(BENCHMARK_WEB_DIR)/playground.wasm" ./cmd/playground
	install -m 0644 web/playground/index.html "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(BENCHMARK_WEB_DIR)/"
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/benchmark-web.mjs "$(BENCHMARK_WEB_DIR)" "$(BENCHMARK_REPEATS)" "$(BENCHMARK_FIXTURE)"

.PHONY: benchmark-collide
benchmark-collide: ## Measure collision with 2,048 bodies natively and in Chromium
	$(GO) build -o "$(BUILD_DIR)/collidebench" ./cmd/collidebench
	i=0; while [ $$i -lt "$(BENCHMARK_REPEATS)" ]; do "$(BUILD_DIR)/collidebench"; i=$$((i + 1)); done
	mkdir -p "$(BUILD_DIR)/collide-web"
	GOOS=js GOARCH=wasm $(GO) build -o "$(BUILD_DIR)/collide-web/collidebench.wasm" ./cmd/collidebench
	install -m 0644 "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(BUILD_DIR)/collide-web/"
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/benchmark-collide.mjs "$(BUILD_DIR)/collide-web" "$(BENCHMARK_REPEATS)"

.PHONY: benchmark-skirmish
benchmark-skirmish: ## Measure a 128 against 128 battle natively, in Node as WebAssembly, and in Chromium
	$(GO) build -o "$(BUILD_DIR)/skirmishbench" ./cmd/skirmishbench
	i=0; while [ $$i -lt "$(BENCHMARK_REPEATS)" ]; do "$(BUILD_DIR)/skirmishbench"; i=$$((i + 1)); done
	GOOS=js GOARCH=wasm $(GO) build -o "$(BUILD_DIR)/skirmishbench.wasm" ./cmd/skirmishbench
	GOOS=js GOARCH=wasm $(GO) run -exec="$$($(GO) env GOROOT)/lib/wasm/go_js_wasm_exec" ./cmd/skirmishbench
	mkdir -p "$(BUILD_DIR)/skirmish-web"
	cp "$(BUILD_DIR)/skirmishbench.wasm" "$(BUILD_DIR)/skirmish-web/"
	install -m 0644 "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" "$(BUILD_DIR)/skirmish-web/"
	PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_CACHE)" $(NODE) scripts/benchmark-collide.mjs "$(BUILD_DIR)/skirmish-web" "$(BENCHMARK_REPEATS)" skirmishbench

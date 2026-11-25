#!/usr/bin/env bash
# Build Beacon with cmd/goje from a separate module, play the desktop and web
# outputs, and check that packaging twice gives identical, verifiable files.
# Usage: scripts/smoke-goje-build.sh. Run `make build-level-editor
# build-tile-maker setup-web` first, and wrap it in `xvfb-run -a` on Linux.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
go=${GO:-go}
node=${NODE:-node}
python=${PYTHON:-python3}
export GOWORK=off PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$repo/.build/playwright}"

mkdir -p .build
tmp=$(mktemp -d "$repo/.build/goje-build.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
goje=$tmp/goje
"$go" build -o "$goje" ./cmd/goje
goos=$("$go" env GOOS)
host=$goos-$("$go" env GOARCH)
go_version=$("$go" list -m -f '{{.GoVersion}}')

game=$tmp/beacon
mkdir -p "$game/assets"
cp examples/beacon/*.go "$game/"
cp examples/beacon/assets/goje.json examples/beacon/assets/tomato.png \
  examples/beacon/assets/tomato.json examples/beacon/assets/toggle.wav "$game/assets/"
"$node" scripts/smoke-beacon.mjs author "$game/assets"
"$go" run ./cmd/projectcheck "$game/assets"

# The smoke host imports internal packages, so the smoke build runs while the
# copy is still inside this module, as in `make smoke-beacon`.
"$goje" build -o "$tmp/smoke" -name beacon -tags goje_smoke,goje_beacon_smoke -target desktop "$game"
smoke=$tmp/smoke/desktop/$host/beacon
if [[ $goos == darwin ]]; then
  smoke=$tmp/smoke/desktop/$host/beacon.app/Contents/MacOS/beacon
fi
"$python" -c 'import subprocess, sys; subprocess.run(sys.argv[1:], check=True, timeout=30)' "$smoke"

rm "$game/run_smoke.go" "$game/run_bench.go"
(
  cd "$game"
  "$go" mod init example.com/beacon
  "$go" mod edit -go="$go_version" -require=github.com/krapht-foundation/goje@v0.0.0 \
    -replace=github.com/krapht-foundation/goje="$repo"
  "$go" mod tidy
  for run in 1 2; do
    "$goje" package -o "$tmp/dist$run" -version 1.0.0
  done
)
"$node" scripts/smoke-beacon.mjs build "$tmp/dist1/web"

release=$tmp/dist1/release
files=(SHA256SUMS beacon-1.0.0-web.zip "beacon-1.0.0-$host.tar.gz")
diff <(ls "$release") <(printf '%s\n' "${files[@]}" | LC_ALL=C sort)
diff <(ls "$release") <(ls "$tmp/dist2/release")
for file in "${files[@]}"; do
  cmp "$release/$file" "$tmp/dist2/release/$file"
done
if command -v sha256sum > /dev/null; then
  (cd "$release" && sha256sum -c SHA256SUMS)
else
  (cd "$release" && shasum -a 256 -c SHA256SUMS)
fi

extracted=$tmp/extracted
mkdir -p "$extracted/web" "$extracted/desktop"
unzip -q "$release/beacon-1.0.0-web.zip" -d "$extracted/web"
tar -xzf "$release/beacon-1.0.0-$host.tar.gz" -C "$extracted/desktop"
diff -r "$tmp/dist1/web" "$extracted/web"
diff -r "$tmp/dist1/desktop/$host" "$extracted/desktop"
if [[ $goos == darwin ]]; then
  plutil -lint "$extracted/desktop/beacon.app/Contents/Info.plist"
  test -x "$extracted/desktop/beacon.app/Contents/MacOS/beacon"
else
  test -x "$extracted/desktop/beacon"
  test -f "$extracted/desktop/beacon.desktop"
  test -f "$extracted/desktop/beacon.png"
fi
echo "passed: goje build and package for Beacon on web and $host: desktop and Chromium smokes," \
  "identical packages from two runs, checksums, extraction, and the bundle files"

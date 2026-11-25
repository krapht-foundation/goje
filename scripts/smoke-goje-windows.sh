#!/usr/bin/env bash
# Package Beacon with cmd/goje on Windows, check that two signed packages are
# identical and verifiable, that the executable is linked as a GUI program, and
# that the extracted game opens its window and keeps running.
# Usage: scripts/smoke-goje-windows.sh, from Git Bash. On a machine without an
# OpenGL 3.3 driver, such as a GitHub-hosted runner, set MESA_DIR to a directory
# holding Mesa's opengl32.dll and the DLLs beside it; they are copied next to the game.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
go=${GO:-go}
python=${PYTHON:-python}
if [[ $("$go" env GOOS) != windows ]]; then
  echo "scripts/smoke-goje-windows.sh runs only on Windows" >&2
  exit 1
fi
host=windows-$("$go" env GOARCH)

mkdir -p .build
tmp=$(mktemp -d "$repo/.build/goje-windows.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
goje=$tmp/goje.exe
"$go" build -o "$goje" ./cmd/goje
"$goje" keygen -o "$tmp/signing"
for run in 1 2; do
  "$goje" package -o "$tmp/dist$run" -name beacon -version 1.0.0 -sign "$tmp/signing.key" ./examples/beacon
done

release=$tmp/dist1/release
files=(SHA256SUMS SHA256SUMS.sig beacon-1.0.0-web.zip "beacon-1.0.0-$host.zip" update.json)
listing() { (cd "$1" && printf '%s\n' *) | LC_ALL=C sort; }
diff <(listing "$release") <(printf '%s\n' "${files[@]}" | LC_ALL=C sort)
diff <(listing "$release") <(listing "$tmp/dist2/release")
for file in "${files[@]}"; do
  cmp "$release/$file" "$tmp/dist2/release/$file"
done
(cd "$release" && sha256sum -c SHA256SUMS)

extracted=$tmp/extracted
"$python" -m zipfile -e "$release/beacon-1.0.0-$host.zip" "$extracted"
diff -r "$tmp/dist1/desktop/$host" "$extracted"
diff <(listing "$extracted") <(echo beacon.exe)

# The optional header's Subsystem field is 2 for a Windows GUI program.
"$python" - "$extracted/beacon.exe" << 'EOF'
import struct, sys
data = open(sys.argv[1], "rb").read()
header = struct.unpack_from("<I", data, 0x3C)[0]
assert data[header:header + 4] == b"PE\0\0", "not a PE executable"
subsystem = struct.unpack_from("<H", data, header + 24 + 68)[0]
assert subsystem == 2, f"subsystem {subsystem}, want 2 (Windows GUI)"
EOF

if [[ -n ${MESA_DIR:-} ]]; then
  cp "$MESA_DIR"/*.dll "$extracted/"
fi
# Check that the exact executable in the archive creates a visible Beacon window.
"$python" - "$extracted/beacon.exe" << 'EOF'
import ctypes
import subprocess
import sys
import time
from ctypes import wintypes

win = ctypes.WinDLL("user32")
callback = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
win.EnumWindows.argtypes = (callback, wintypes.LPARAM)
win.GetWindowThreadProcessId.argtypes = (wintypes.HWND, ctypes.POINTER(wintypes.DWORD))
win.IsWindowVisible.argtypes = (wintypes.HWND,)
win.GetWindowTextW.argtypes = (wintypes.HWND, wintypes.LPWSTR, ctypes.c_int)

with subprocess.Popen([sys.argv[1]], stderr=subprocess.PIPE) as game:
    try:
        visible = False

        @callback
        def check_window(handle, _):
            global visible
            pid = wintypes.DWORD()
            win.GetWindowThreadProcessId(handle, ctypes.byref(pid))
            if pid.value == game.pid and win.IsWindowVisible(handle):
                title = ctypes.create_unicode_buffer(128)
                win.GetWindowTextW(handle, title, len(title))
                if title.value.startswith("Goje Beacon"):
                    visible = True
            return True

        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if game.poll() is not None:
                sys.exit(f"Beacon exited with status {game.returncode}:\n"
                         + game.stderr.read().decode(errors="replace"))
            win.EnumWindows(check_window, 0)
            if visible:
                break
            time.sleep(0.1)
        else:
            sys.exit("Beacon did not open a visible window within 15 seconds")
    finally:
        game.terminate()
        game.wait()
EOF
echo "passed: goje package for Beacon on $host: identical signed packages from two runs," \
  "checksums, extraction, a GUI executable, and a visible Beacon window"

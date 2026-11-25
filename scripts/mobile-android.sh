#!/usr/bin/env bash
# Generate Beacon's Android project with `goje mobile`, check that a second run keeps a
# native change, build a release AAB signed with a new disposable upload key, verify the
# signature and contents, and derive a universal APK from it. `emulator` then installs that
# APK on the running emulator with its network off and plays the challenge by touch.
# Usage: scripts/mobile-android.sh build, then scripts/mobile-android.sh emulator.
# Needs Go, Node 22 or later, JDK 21, and ANDROID_HOME with build tools; see specs/mobile-m18.md.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
go=${GO:-go}
node=${NODE:-node}
out=${MOBILE_OUT:-$repo/.build/mobile-android}
id=io.github.kraphtfoundation.beacon
project=$out/beacon/mobile/android
apk=$out/universal.apk

build() {
  mkdir -p "$(dirname "$out")"
  mkdir "$out"
  "$go" build -o "$out/goje" ./cmd/goje
  local goje=$out/goje
  "$goje" mobile -o "$out/beacon" -name beacon -version 1.0.0 -build 1 -id "$id" -platform android ./examples/beacon

  # A developer's native change survives a rebuild, and the Goje-owned fields follow the flags.
  local activity=$project/app/src/main/java/io/github/kraphtfoundation/beacon/MainActivity.java
  echo '// Native change kept by goje mobile.' >> "$activity"
  "$goje" mobile -o "$out/beacon" -name beacon -version 1.0.1 -build 2 -id "$id" -platform android ./examples/beacon
  grep -q 'Native change kept by goje mobile' "$activity"
  grep -qE '^\s*versionCode 2$' "$project/app/build.gradle"
  grep -qE '^\s*versionName "1.0.1"$' "$project/app/build.gradle"
  if "$goje" mobile -o "$out/beacon" -version 1.0.1 -build 2 -id io.example.other -platform android ./examples/beacon; then
    echo "goje mobile accepted an application ID that differs from the project's" >&2
    exit 1
  fi
  # cap sync adds Capacitor's two Cordova shim scripts beside the web build.
  diff -r -x cordova.js -x cordova_plugins.js "$out/beacon/web" "$project/app/src/main/assets/public"

  # The disposable upload key exists only for this run.
  local keystore=$out/upload.jks password
  password=$(openssl rand -hex 16)
  trap 'rm -f "$out/upload.jks"' EXIT
  keytool -genkeypair -keystore "$keystore" -storetype PKCS12 -storepass "$password" -alias upload \
    -keyalg RSA -keysize 3072 -validity 365 -dname 'CN=Goje disposable upload key' > /dev/null
  (
    cd "$project"
    ./gradlew --no-daemon --console=plain bundleRelease \
      -Pandroid.injected.signing.store.file="$keystore" -Pandroid.injected.signing.store.password="$password" \
      -Pandroid.injected.signing.key.alias=upload -Pandroid.injected.signing.key.password="$password"
  )
  local aab=$project/app/build/outputs/bundle/release/app-release.aab
  # Trust only this run's self-signed upload key for verification.
  jarsigner -verify -strict -keystore "$keystore" -storepass "$password" "$aab" upload
  local expected signer
  expected=$(keytool -list -v -keystore "$keystore" -storepass "$password" -alias upload | grep 'SHA256:' | awk '{print $2}')
  signer=$(keytool -printcert -jarfile "$aab" | grep 'SHA256:' | awk '{print $2}')
  [[ -n $expected && $signer == "$expected" ]]

  local bundletool=$out/bundletool.jar
  curl -fsSLo "$bundletool" https://github.com/google/bundletool/releases/download/1.18.3/bundletool-all-1.18.3.jar
  echo "a099cfa1543f55593bc2ed16a70a7c67fe54b1747bb7301f37fdfd6d91028e29  $bundletool" | sha256sum -c -
  java -jar "$bundletool" validate --bundle="$aab"
  java -jar "$bundletool" build-apks --mode=universal --bundle="$aab" --output="$out/beacon.apks" \
    --ks="$keystore" --ks-pass="pass:$password" --ks-key-alias=upload --key-pass="pass:$password"
  unzip -p "$out/beacon.apks" universal.apk > "$apk"

  local tools
  tools=$(find "$ANDROID_HOME/build-tools" -mindepth 1 -maxdepth 1 | sort -V | tail -1)
  "$tools/apksigner" verify --print-certs "$apk" | grep -qi "SHA-256 digest: ${expected//:/}"
  # The wrapper holds the exact web build, no plugin, no remote server, and only the template's permissions.
  mkdir "$out/apk"
  unzip -q "$apk" -d "$out/apk"
  diff -r -x cordova.js -x cordova_plugins.js "$out/beacon/web" "$out/apk/assets/public"
  [[ $(tr -d ' \n' < "$out/apk/assets/capacitor.plugins.json") == '[]' ]]
  "$node" -e 'const c = JSON.parse(require("fs").readFileSync(process.argv[1]));
    if (c.server || c.appId !== process.argv[2]) throw new Error("unexpected Capacitor config");' \
    "$out/apk/assets/capacitor.config.json" "$id"
  diff <("$tools/aapt2" dump permissions "$apk" | sed -n "s/^uses-permission: name='\(.*\)'.*/\1/p" | sort) \
    <(printf '%s\n' android.permission.INTERNET "$id.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION" | sort)
  echo "passed: Beacon's Android project, a kept native change, and a release AAB signed by a disposable" \
    "upload key with verified signatures and contents"
}

emulator() {
  local serial
  serial=$(adb devices | awk '$1 ~ /^emulator-[0-9]+$/ && $2 == "device" { print $1; exit }')
  if [[ -z $serial ]]; then
    echo "No running Android emulator; this smoke never changes a physical device's network." >&2
    exit 1
  fi
  adb -s "$serial" wait-for-device
  adb -s "$serial" shell svc wifi disable
  adb -s "$serial" shell svc data disable
  adb -s "$serial" shell cmd connectivity airplane-mode enable
  adb -s "$serial" shell svc power stayon true
  adb -s "$serial" shell input keyevent KEYCODE_WAKEUP
  adb -s "$serial" shell wm dismiss-keyguard
  adb -s "$serial" install -r "$apk"
  adb -s "$serial" shell am start -W -n "$id/.MainActivity"
  "$node" scripts/smoke-mobile.mjs android "$serial"
  adb -s "$serial" shell pidof "$id" > /dev/null
  adb -s "$serial" exec-out screencap -p > "$out/emulator.png"
  echo "passed: the APK derived from the signed AAB launched offline and played Beacon by touch"
}

case ${1:-} in
build) build ;;
emulator) emulator ;;
*)
  echo "usage: $0 build|emulator" >&2
  exit 2
  ;;
esac

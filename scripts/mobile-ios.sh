#!/usr/bin/env bash
# iOS gates for `goje mobile` on a Mac with Xcode, never with a store upload.
#
# scripts/mobile-ios.sh simulator
#   Generates Beacon's iOS project, checks that a second run keeps a native change, builds it
#   for the simulator without signing, checks the bundle, installs it on a new simulator, and
#   plays the challenge by touch with AXe.
#
# scripts/mobile-ios.sh archive
#   Archives and exports a distribution-signed IPA with credentials the caller supplies, then
#   verifies its signature, team, and profile. It needs these environment variables:
#     IOS_APP_ID            the bundle ID registered for the team
#     APPLE_TEAM_ID         the ten-character team ID
#     IOS_CERTIFICATE_P12   path to the Apple Distribution certificate and key, as PKCS #12
#     IOS_CERTIFICATE_PASSWORD
#     IOS_PROFILE           path to the App Store provisioning profile for IOS_APP_ID
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
go=${GO:-go}
node=${NODE:-node}
out=${MOBILE_OUT:-$repo/.build/mobile-ios}
project=$out/beacon/mobile/ios/App
axe_url=https://github.com/cameroncooke/AXe/releases/download/v2.2.0/AXe-macOS-v2.2.0-universal.tar.gz
axe_sha256=456772eeec29cc3a4bcdf7f8fa46dfea39aa5e21350feea027e7e7c2f1c76922

generate() { # ID VERSION BUILD
  "$out/goje" mobile -o "$out/beacon" -name beacon -version "$2" -build "$3" -id "$1" -platform ios ./examples/beacon
}

plist() { # KEY FILE
  plutil -extract "$1" raw -o - "$2"
}

prepare() {
  mkdir -p "$(dirname "$out")"
  mkdir "$out"
  "$go" build -o "$out/goje" ./cmd/goje
}

simulator() {
  local id=io.github.kraphtfoundation.beacon
  prepare
  generate "$id" 1.0.0 1
  echo '// Native change kept by goje mobile.' >> "$project/App/AppDelegate.swift"
  generate "$id" 1.0.1 2
  grep -q 'Native change kept by goje mobile' "$project/App/AppDelegate.swift"
  if generate io.example.other 1.0.1 2; then
    echo "goje mobile accepted a bundle ID that differs from the project's" >&2
    exit 1
  fi

  xcodebuild -project "$project/App.xcodeproj" -scheme App -configuration Release -sdk iphonesimulator \
    -destination 'generic/platform=iOS Simulator' -derivedDataPath "$out/derived" CODE_SIGNING_ALLOWED=NO build
  local app=$out/derived/Build/Products/Release-iphonesimulator/App.app
  [[ $(plist CFBundleIdentifier "$app/Info.plist") == "$id" ]]
  [[ $(plist CFBundleShortVersionString "$app/Info.plist") == 1.0.1 ]]
  [[ $(plist CFBundleVersion "$app/Info.plist") == 2 ]]
  # The app holds the exact web build, no plugin, and no remote server.
  # cap sync adds Capacitor's two Cordova shim scripts beside the web build.
  diff -r -x cordova.js -x cordova_plugins.js "$out/beacon/web" "$app/public"
  "$node" -e 'const c = JSON.parse(require("fs").readFileSync(process.argv[1]));
    if (c.server || c.appId !== process.argv[2] || (c.packageClassList ?? []).length) {
      throw new Error("unexpected Capacitor config");
    }' "$app/capacitor.config.json" "$id"

  curl -fsSLo "$out/axe.tar.gz" "$axe_url"
  echo "$axe_sha256  $out/axe.tar.gz" | shasum -a 256 -c -
  mkdir "$out/axe"
  tar -xzf "$out/axe.tar.gz" -C "$out/axe"

  local runtime type
  runtime=$(xcrun simctl list runtimes available -j |
    "$node" -e 'const r = JSON.parse(require("fs").readFileSync(0)).runtimes.filter(r => r.platform === "iOS" && r.isAvailable);
      r.sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }));
      if (!r.length) throw new Error("No installed iOS simulator runtime");
      console.log(r[0].identifier)')
  type=$(xcrun simctl list devicetypes -j |
    "$node" -e 'const t = JSON.parse(require("fs").readFileSync(0)).devicetypes
      .filter(t => t.productFamily === "iPhone");
      t.sort((a, b) => b.minRuntimeVersion - a.minRuntimeVersion);
      if (!t.length) throw new Error("No iPhone simulator device types");
      console.log(t[0].identifier)')
  udid=$(xcrun simctl create goje-beacon "$type" "$runtime")
  trap 'xcrun simctl delete "$udid" || true' EXIT
  xcrun simctl bootstatus "$udid" -b
  xcrun simctl install "$udid" "$app"
  xcrun simctl launch "$udid" "$id"
  "$node" scripts/smoke-mobile.mjs ios "$udid" "$out/axe/axe"
  xcrun simctl io "$udid" screenshot "$out/simulator.png"
  echo "passed: Beacon's iOS project, a kept native change, a simulator build with the exact web" \
    "bundle, and the challenge won and restarted by touch"
}

archive() {
  : "${IOS_APP_ID:?}" "${APPLE_TEAM_ID:?}" "${IOS_CERTIFICATE_P12:?}" "${IOS_CERTIFICATE_PASSWORD:?}" "${IOS_PROFILE:?}"
  prepare
  generate "$IOS_APP_ID" 1.0.0 1

  # A temporary keychain holds the certificate only for this run. The EXIT trap reads these
  # variables after the function returns, so they are global.
  keychain=$out/signing.keychain-db
  password=$(openssl rand -hex 16)
  profiles="$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles"
  security cms -D -i "$IOS_PROFILE" > "$out/profile.plist"
  uuid=$(plist UUID "$out/profile.plist")
  [[ $(plist TeamIdentifier.0 "$out/profile.plist") == "$APPLE_TEAM_ID" ]]
  local existing
  existing=$(security list-keychains -d user)
  keychains=()
  while IFS= read -r line; do
    [[ $line =~ \"([^\"]+)\" ]] && keychains+=("${BASH_REMATCH[1]}")
  done <<< "$existing"
  installed=
  trap 'security list-keychains -d user -s ${keychains[@]+"${keychains[@]}"} || true
    security delete-keychain "$keychain" || true
    [[ -z $installed ]] || rm -f "$installed"' EXIT
  # Remove the profile afterward only if this run installed it.
  mkdir -p "$profiles"
  if [[ ! -e "$profiles/$uuid.mobileprovision" ]]; then
    installed=$profiles/$uuid.mobileprovision
    cp "$IOS_PROFILE" "$installed"
  fi
  security create-keychain -p "$password" "$keychain"
  security set-keychain-settings -lut 3600 "$keychain"
  security unlock-keychain -p "$password" "$keychain"
  security import "$IOS_CERTIFICATE_P12" -k "$keychain" -P "$IOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$password" "$keychain" > /dev/null
  security list-keychains -d user -s "$keychain" ${keychains[@]+"${keychains[@]}"}

  xcodebuild -project "$project/App.xcodeproj" -scheme App -configuration Release \
    -destination 'generic/platform=iOS' -archivePath "$out/Beacon.xcarchive" archive \
    DEVELOPMENT_TEAM="$APPLE_TEAM_ID" CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY='Apple Distribution' \
    PROVISIONING_PROFILE_SPECIFIER="$uuid" OTHER_CODE_SIGN_FLAGS="--keychain $keychain"
  cat > "$out/export.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>method</key><string>app-store-connect</string>
	<key>destination</key><string>export</string>
	<key>teamID</key><string>$APPLE_TEAM_ID</string>
	<key>signingStyle</key><string>manual</string>
	<key>signingCertificate</key><string>Apple Distribution</string>
	<key>provisioningProfiles</key><dict><key>$IOS_APP_ID</key><string>$uuid</string></dict>
</dict>
</plist>
EOF
  xcodebuild -exportArchive -archivePath "$out/Beacon.xcarchive" -exportOptionsPlist "$out/export.plist" \
    -exportPath "$out/export"

  local ipa app
  ipa=$(find "$out/export" -name '*.ipa' | head -1)
  mkdir "$out/ipa"
  unzip -q "$ipa" -d "$out/ipa"
  app=$(find "$out/ipa/Payload" -maxdepth 1 -name '*.app' | head -1)
  codesign --verify --deep --strict --verbose=2 "$app"
  codesign -dvv "$app" 2> "$out/codesign.txt"
  grep -q '^Authority=Apple Distribution' "$out/codesign.txt"
  grep -qx "TeamIdentifier=$APPLE_TEAM_ID" "$out/codesign.txt"
  security cms -D -i "$app/embedded.mobileprovision" > "$out/embedded.plist"
  [[ $(plist UUID "$out/embedded.plist") == "$uuid" ]]
  [[ $(plist CFBundleIdentifier "$app/Info.plist") == "$IOS_APP_ID" ]]
  diff -r -x cordova.js -x cordova_plugins.js "$out/beacon/web" "$app/public"
  echo "passed: a distribution-signed iOS archive and IPA for $IOS_APP_ID, team $APPLE_TEAM_ID," \
    "profile $uuid; nothing was uploaded"
}

case ${1:-} in
simulator) simulator ;;
archive) archive ;;
*)
  echo "usage: $0 simulator|archive" >&2
  exit 2
  ;;
esac

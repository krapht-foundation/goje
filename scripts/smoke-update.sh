#!/usr/bin/env bash
# Verify the hosted Beacon staging channel described in specs/update-m18.md over
# real HTTPS: the fixed channel manifest names VERSION and verifies with the
# staging public key, one native archive downloads from its versioned URL, and
# an older manifest, a wrong key, and a repeated download are refused without
# leaving a file. With PREVIOUS, the previous version's signed manifest and
# archive must still verify, so publishing VERSION did not change them.
# Usage: scripts/smoke-update.sh PUBKEY VERSION [PREVIOUS]
# Environment: REPO (default krapht-foundation/goje), NAME (default goje-beacon),
# PLATFORM (default linux-amd64), GO (default go).
set -euo pipefail

if (($# < 2 || $# > 3)); then
  echo "usage: $0 PUBKEY VERSION [PREVIOUS]" >&2
  exit 2
fi
pub=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
version=$2
previous=${3:-}
repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
go=${GO:-go}
github=https://github.com/${REPO:-krapht-foundation/goje}/releases/download
name=${NAME:-goje-beacon}
platform=${PLATFORM:-linux-amd64}
ext=.tar.gz
[[ $platform == windows-* ]] && ext=.zip
channel=$github/beacon-staging/update.json
artifacts="$github/beacon-staging-v{version}/{file}"
export GOWORK=off

mkdir -p .build
tmp=$(mktemp -d "$repo/.build/update-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
goje=$tmp/goje
"$go" build -o "$goje" ./cmd/goje
"$goje" keygen -o "$tmp/wrong"

check() { "$goje" update -name "$name" -artifacts "$artifacts" "$@"; }
# refuse runs a command that must fail with a message containing $1.
refuse() {
  local want=$1 out
  shift
  if out=$("$@" 2>&1); then
    echo "update smoke: accepted, want failure \"$want\": $*" >&2
    exit 1
  fi
  if [[ $out != *"$want"* ]]; then
    echo "update smoke: failed with \"$out\", want \"$want\"" >&2
    exit 1
  fi
}
# empty fails unless the directory $1 holds no file.
empty() {
  if [[ -n $(ls -A "$1") ]]; then
    echo "update smoke: $1 holds an unverified file" >&2
    exit 1
  fi
}

out=$(check -manifest "$channel" -key "$pub" -current 0.0.0 2>&1)
echo "$out"
if [[ $out != *"verified $name $version"* ]]; then
  echo "update smoke: the channel does not name $name $version" >&2
  exit 1
fi
file=$name-$version-$platform$ext
mkdir "$tmp/new"
check -manifest "$channel" -key "$pub" -current 0.0.0 -file "$file" -o "$tmp/new"
test -f "$tmp/new/$file"
refuse "already exists" check -manifest "$channel" -key "$pub" -current 0.0.0 -file "$file" -o "$tmp/new"
refuse "not newer" check -manifest "$channel" -key "$pub" -current "$version"
mkdir "$tmp/wrong-key"
refuse "signature does not match" \
  check -manifest "$channel" -key "$tmp/wrong.pub" -current 0.0.0 -file "$file" -o "$tmp/wrong-key"
empty "$tmp/wrong-key"
mkdir "$tmp/missing"
refuse "has no file" check -manifest "$channel" -key "$pub" -current 0.0.0 \
  -file "$name-$version-plan9-amd64.tar.gz" -o "$tmp/missing"
empty "$tmp/missing"

if [[ -n $previous ]]; then
  # The previous version's own manifest and archive URLs must still name its bytes.
  old=$github/beacon-staging-v$previous/update.json
  old_file=$name-$previous-$platform$ext
  mkdir "$tmp/old"
  check -manifest "$old" -key "$pub" -current 0.0.0 -file "$old_file" -o "$tmp/old"
  if cmp -s "$tmp/old/$old_file" "$tmp/new/$file"; then
    echo "update smoke: $previous and $version have the same archive" >&2
    exit 1
  fi
  refuse "not newer" check -manifest "$old" -key "$pub" -current "$version"
fi
echo "update smoke: $name $version verified on $channel"

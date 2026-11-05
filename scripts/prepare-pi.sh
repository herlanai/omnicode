#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
case "$(uname -m)" in arm64) arch=arm64;; x86_64) arch=x64;; *) echo 'Unsupported macOS architecture' >&2; exit 1;; esac
node_version=24.15.0
runtime="$PWD/bin/pi-runtime"
cache="$PWD/bin/runtime-cache"
mkdir -p "$cache"
fingerprint="$(shasum -a 256 packaging/pi/package-lock.json | awk '{print $1}')-$node_version-$arch"
if [ -f "$runtime/.manifest" ] && [ "$(cat "$runtime/.manifest")" = "$fingerprint" ]; then
  env PATH=/usr/bin:/bin "$runtime/bin/node" "$runtime/node_modules/@earendil-works/pi-coding-agent/dist/cli.js" --version
  exit 0
fi
archive="node-v${node_version}-darwin-${arch}.tar.gz"
if [ ! -f "$cache/$archive" ]; then
  curl -fsSL --retry 2 "https://nodejs.org/dist/v$node_version/$archive" -o "$cache/$archive.part"
  mv "$cache/$archive.part" "$cache/$archive"
fi
expected=$(awk -v name="$archive" '$2==name {print $1}' packaging/pi/node-shasums.txt)
[ -n "$expected" ]
actual=$(shasum -a 256 "$cache/$archive" | awk '{print $1}')
[ "$actual" = "$expected" ] || { echo 'Node checksum mismatch' >&2; exit 1; }
stage=$(mktemp -d "$PWD/bin/pi-stage.XXXXXX")
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/extract" "$stage/runtime" "$stage/runtime/bin"
tar -xzf "$cache/$archive" -C "$stage/extract"
cp "$stage/extract/node-v${node_version}-darwin-${arch}/bin/node" "$stage/runtime/bin/node"
cp "$stage/extract/node-v${node_version}-darwin-${arch}/LICENSE" "$stage/runtime/NODE-LICENSE"
cp packaging/pi/package.json packaging/pi/package-lock.json "$stage/runtime/"
npm ci --prefix "$stage/runtime" --ignore-scripts --omit=dev --no-audit --no-fund --cache "$cache/npm"
env PATH=/usr/bin:/bin "$stage/runtime/bin/node" "$stage/runtime/node_modules/@earendil-works/pi-coding-agent/dist/cli.js" --version
printf '%s' "$fingerprint" > "$stage/runtime/.manifest"
rm -rf "$runtime"
mv "$stage/runtime" "$runtime"

#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
iconset=bin/Omnicode.iconset
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" assets/omnicode-icon.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
  doubled=$((size * 2))
  sips -z "$doubled" "$doubled" assets/omnicode-icon.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o bin/Omnicode.icns

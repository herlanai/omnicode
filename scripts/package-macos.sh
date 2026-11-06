#!/bin/bash
set -eu
cd "$(dirname "$0")/.."
# Build a fresh bundle; never retain metadata or signatures from an earlier build.
stage=$(mktemp -d "$PWD/bin/.omnicode-package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
app="$stage/Omnicode.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp bin/omnicode "$app/Contents/MacOS/omnicode"
cp packaging/Info.plist "$app/Contents/Info.plist"
cp bin/Omnicode.icns "$app/Contents/Resources/OmnicodeIcon-v3.icns"
go run ./cmd/bundle-terminal "$app/Contents/Resources"
ditto bin/pi-runtime "$app/Contents/Resources/pi"
(cd resources/open-code-review && shasum -a 256 -c SHA256SUMS)
ditto resources/open-code-review "$app/Contents/Resources/open-code-review"
codesign --force --sign - "$app/Contents/Resources/open-code-review/opencodereview"
ditto bin/lsp-runtime "$app/Contents/Resources/lsp"
codesign --force --sign - "$app/Contents/Resources/lsp/bin/gopls"
# Seal every bundled native executable/library, including optional npm addons.
find "$app/Contents/Resources/pi" -type f \( -perm -111 -o -name '*.node' -o -name '*.dylib' \) -print0 | while IFS= read -r -d '' file; do
  if file -b "$file" | grep -q "Mach-O"; then codesign --force --sign - "$file"; fi
done
env PATH=/usr/bin:/bin "$app/Contents/Resources/pi/bin/node" "$app/Contents/Resources/pi/node_modules/@earendil-works/pi-coding-agent/dist/cli.js" --version
# Sign nested native code before sealing the complete bundle with a local signature.
codesign --force --sign - "$app/Contents/Resources/libghostty-vt.dylib"
codesign --force --sign - "$app/Contents/Resources/libgolet-sqlite3.dylib"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"
plutil -lint "$app/Contents/Info.plist"
# Replace only generated build outputs after the new bundle passes validation.
rm -rf bin/Omnicode.app
mv "$app" bin/Omnicode.app
# Distribute a drag-to-install disk image, including an Applications shortcut.
installer="$stage/installer"
mkdir "$installer"
ditto bin/Omnicode.app "$installer/Omnicode.app"
ln -s /Applications "$installer/Applications"
arch=$(uname -m)
image="bin/Omnicode-macOS-${arch}.dmg"
hdiutil create -volname Omnicode -srcfolder "$installer" -format UDZO -ov "$stage/Omnicode.dmg"
hdiutil verify "$stage/Omnicode.dmg"
# Verify the app and icon inside the actual read-only installation image.
mount="$stage/mounted"
mkdir "$mount"
hdiutil attach "$stage/Omnicode.dmg" -readonly -nobrowse -mountpoint "$mount"
trap 'hdiutil detach "$mount" >/dev/null 2>&1 || true; rm -rf "$stage"' EXIT
codesign --verify --deep --strict "$mount/Omnicode.app"
env PATH=/usr/bin:/bin "$mount/Omnicode.app/Contents/Resources/pi/bin/node" "$mount/Omnicode.app/Contents/Resources/pi/node_modules/@earendil-works/pi-coding-agent/dist/cli.js" --version
"$mount/Omnicode.app/Contents/Resources/lsp/bin/gopls" version
"$mount/Omnicode.app/Contents/Resources/open-code-review/opencodereview" --version
test -f "$mount/Omnicode.app/Contents/Resources/open-code-review/LICENSE"
test -f "$mount/Omnicode.app/Contents/Resources/lsp/node_modules/typescript/lib/tsserver.js"
cmp bin/Omnicode.icns "$mount/Omnicode.app/Contents/Resources/OmnicodeIcon-v3.icns"
test "$(readlink "$mount/Applications")" = /Applications
hdiutil detach "$mount"
trap 'rm -rf "$stage"' EXIT
# The staging directory is on the destination volume, so publication is atomic.
mv -f "$stage/Omnicode.dmg" "$image"
for old in bin/Omnicode-macOS-"$arch"-build*.dmg; do
  [[ -f "$old" && ! -L "$old" ]] || continue
  build=${old##*-build}
  [[ ${build%.dmg} =~ ^[0-9]+$ ]] || continue
  rm -f "$old"
done
printf 'Installer: %s\n' "$image"

#!/bin/bash
set -eu
cd "$(dirname "$0")/.."
source scripts/macos-target.sh
# Build a fresh bundle; never retain metadata or signatures from an earlier build.
stage=$(mktemp -d "$PWD/bin/.omnicode-package.XXXXXX")
mount=""
cleanup() {
  if [[ -n "$mount" ]]; then hdiutil detach "$mount" >/dev/null 2>&1 || true; fi
  rm -rf "$stage"
}
trap cleanup EXIT
app="$stage/Omnicode.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$build_dir/omnicode" "$app/Contents/MacOS/omnicode"
cp packaging/Info.plist "$app/Contents/Info.plist"
cp resources/omnix-LICENSE "$app/Contents/Resources/omnix-LICENSE"
cp resources/mysql-LICENSE resources/edwards25519-LICENSE "$app/Contents/Resources/"
cp bin/Omnicode.icns "$app/Contents/Resources/OmnicodeIcon-v3.icns"
module=$(go list -m -f '{{.Dir}}' github.com/HyperparameterSI/golet)
python3 scripts/prepare-native.py "$go_arch" "$app/Contents/Resources" "$module"
ditto "$build_dir/pi-runtime" "$app/Contents/Resources/pi"
codesign --force --sign - "$app/Contents/Resources/open-code-review/opencodereview"
ditto "$build_dir/lsp-runtime" "$app/Contents/Resources/lsp"
codesign --force --sign - "$app/Contents/Resources/lsp/bin/gopls"
# Seal every bundled native executable/library, including optional npm addons.
find "$app/Contents/Resources/pi" -type f \( -perm -111 -o -name '*.node' -o -name '*.dylib' \) -print0 | while IFS= read -r -d '' file; do
  if file -b "$file" | grep -q "Mach-O"; then codesign --force --sign - "$file"; fi
done
if [[ "$arch" = "$(uname -m)" ]]; then
  env PATH=/usr/bin:/bin "$app/Contents/Resources/pi/bin/node" "$app/Contents/Resources/pi/node_modules/@earendil-works/pi-coding-agent/dist/cli.js" --version
fi
# Sign nested native code before sealing the complete bundle with a local signature.
codesign --force --sign - "$app/Contents/Resources/libghostty-vt.dylib"
codesign --force --sign - "$app/Contents/Resources/libgolet-sqlite3.dylib"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"
plutil -lint "$app/Contents/Info.plist"
# Replace only generated build outputs after the new bundle passes validation.
rm -rf "$build_dir/Omnicode.app"
mv "$app" "$build_dir/Omnicode.app"
app="$build_dir/Omnicode.app"
for binary in "$app/Contents/MacOS/omnicode" "$app/Contents/Resources/"*.dylib "$app/Contents/Resources/pi/bin/node" "$app/Contents/Resources/lsp/bin/gopls" "$app/Contents/Resources/open-code-review/opencodereview"; do
  lipo -verify_arch "$arch" "$binary"
done
pi_modules="$app/Contents/Resources/pi/node_modules/@earendil-works/pi-coding-agent/node_modules"
lipo -verify_arch "$arch" "$pi_modules/@earendil-works/pi-tui/native/darwin/prebuilds/darwin-$node_arch/darwin-modifiers.node"
lipo -verify_arch "$arch" "$pi_modules/@mariozechner/clipboard-darwin-$node_arch/clipboard.darwin-$node_arch.node"
# Drag-to-install disk image containing the signed application bundle.
installer="$stage/installer"
mkdir "$installer"
ditto "$app" "$installer/Omnicode.app"
ln -s /Applications "$installer/Applications"
mkdir "$installer/.background"
swift packaging/dmg/background.swift assets/omnicode-icon.png "$installer/.background/installer.png"
image="bin/Omnicode-macOS-${arch}.dmg"
# Finder metadata and background are created on the final volume before compression.
hdiutil create -volname "Omnicode-${stage##*.}" -srcfolder "$installer" -fs HFS+ -format UDRW -ov "$stage/writable.dmg"
mount="$stage/mounted"
mkdir "$mount"
hdiutil attach "$stage/writable.dmg" -nobrowse -mountpoint "$mount"
osascript packaging/dmg/layout.applescript "$mount"
# A unique staging name avoids Finder confusing an already mounted installer.
diskutil renameVolume "$mount" Omnicode
cp bin/Omnicode.icns "$mount/.VolumeIcon.icns"
xcrun SetFile -a C "$mount"
cmp bin/Omnicode.icns "$mount/.VolumeIcon.icns"
test -s "$mount/.DS_Store"
sync
hdiutil detach "$mount"
mount=""
hdiutil convert "$stage/writable.dmg" -format UDZO -o "$stage/Omnicode.dmg"
hdiutil verify "$stage/Omnicode.dmg"
# Verify the app and icon inside the actual read-only installation image.
mount="$stage/mounted"
mkdir -p "$mount"
hdiutil attach "$stage/Omnicode.dmg" -readonly -nobrowse -mountpoint "$mount"
test -s "$mount/.DS_Store"
cmp "$installer/.background/installer.png" "$mount/.background/installer.png"
cmp bin/Omnicode.icns "$mount/.VolumeIcon.icns"
codesign --verify --deep --strict "$mount/Omnicode.app"
cmp "$app/Contents/MacOS/omnicode" "$mount/Omnicode.app/Contents/MacOS/omnicode"
cmp bin/Omnicode.icns "$mount/Omnicode.app/Contents/Resources/OmnicodeIcon-v3.icns"
lipo -verify_arch "$arch" "$mount/Omnicode.app/Contents/MacOS/omnicode"
if [[ "$arch" = "$(uname -m)" ]]; then
  env PATH=/usr/bin:/bin "$app/Contents/Resources/pi/bin/node" "$app/Contents/Resources/pi/node_modules/@earendil-works/pi-coding-agent/dist/cli.js" --version
  "$app/Contents/Resources/lsp/bin/gopls" version
  "$app/Contents/Resources/open-code-review/opencodereview" --version
else
  printf 'Cross-built %s: architecture/signature checks passed; execution requires an Intel Mac.\n' "$arch"
fi
test "$(readlink "$mount/Applications")" = /Applications
hdiutil detach "$mount"
mount=""
# Set the DMG file icon as well as the application and mounted-volume icons.
swift packaging/dmg/file-icon.swift bin/Omnicode.icns "$stage/Omnicode.dmg"
xattr -p com.apple.ResourceFork "$stage/Omnicode.dmg" >/dev/null
xattr -p com.apple.FinderInfo "$stage/Omnicode.dmg" >/dev/null
# The staging directory is on the destination volume, so publication is atomic.
mv -f "$stage/Omnicode.dmg" "$image"
xattr -p com.apple.ResourceFork "$image" >/dev/null
xattr -p com.apple.FinderInfo "$image" >/dev/null
for old in bin/Omnicode-macOS-"$arch"-build*.dmg; do
  [[ -f "$old" && ! -L "$old" ]] || continue
  build=${old##*-build}
  [[ ${build%.dmg} =~ ^[0-9]+$ ]] || continue
  rm -f "$old"
done
printf 'Installer: %s\n' "$image"

# Keep the original development launch path current after architecture-specific builds.
# Archive an older real bundle instead of deleting a potentially running copy.
if [[ "$arch" = "$(uname -m)" ]]; then
  legacy="$PWD/bin/Omnicode.app"
  if [[ -e "$legacy" && ! -L "$legacy" ]]; then
    previous=$(mktemp -d "$PWD/bin/.previous-omnicode.XXXXXX")
    mv "$legacy" "$previous/Omnicode.app"
  fi
  ln -sfn "macos-$arch/Omnicode.app" "$legacy"
fi

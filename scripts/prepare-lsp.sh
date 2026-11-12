#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./scripts/macos-target.sh
runtime="$build_dir/lsp-runtime"
version=v0.24.0
fingerprint="$(shasum -a 256 packaging/lsp/package-lock.json | awk '{print $1}')-$version-${arch}"
if [ -f "$runtime/.manifest" ] && [ "$(cat "$runtime/.manifest")" = "$fingerprint" ]; then exit 0; fi
stage=$(mktemp -d "$PWD/bin/lsp-stage.XXXXXX")
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/bin"
go mod download "golang.org/x/tools/gopls@$version"
GOOS=darwin GOARCH="$go_arch" CGO_ENABLED=0 go build -C "$(go env GOPATH)/pkg/mod/golang.org/x/tools/gopls@$version" -o "$stage/bin/gopls" .
cp "$(go env GOPATH)/pkg/mod/golang.org/x/tools/gopls@$version/LICENSE" "$stage/GOPLS-LICENSE"
cp packaging/lsp/package*.json "$stage/"
npm ci --prefix "$stage" --ignore-scripts --omit=dev --no-audit --no-fund --cache "$PWD/bin/runtime-cache/npm"
printf '%s' "$fingerprint" > "$stage/.manifest"
rm -rf "$runtime"
mv "$stage" "$runtime"

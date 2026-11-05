#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
runtime="$PWD/bin/lsp-runtime"
version=v0.24.0
fingerprint="$(shasum -a 256 packaging/lsp/package-lock.json | awk '{print $1}')-$version-$(uname -m)"
if [ -f "$runtime/.manifest" ] && [ "$(cat "$runtime/.manifest")" = "$fingerprint" ]; then exit 0; fi
stage=$(mktemp -d "$PWD/bin/lsp-stage.XXXXXX")
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/bin"
GOBIN="$stage/bin" go install "golang.org/x/tools/gopls@$version"
cp "$(go env GOPATH)/pkg/mod/golang.org/x/tools/gopls@$version/LICENSE" "$stage/GOPLS-LICENSE"
cp packaging/lsp/package*.json "$stage/"
npm ci --prefix "$stage" --ignore-scripts --omit=dev --no-audit --no-fund --cache "$PWD/bin/runtime-cache/npm"
printf '%s' "$fingerprint" > "$stage/.manifest"
rm -rf "$runtime"
mv "$stage" "$runtime"

#!/bin/bash
set -eu
cd "$(dirname "$0")/.."
source scripts/macos-target.sh
GOOS=darwin GOARCH="$go_arch" CGO_ENABLED=0 go build -o "$build_dir/omnicode" .
sh scripts/build-icon.sh
sh scripts/prepare-pi.sh
sh scripts/prepare-lsp.sh
bash scripts/package-macos.sh

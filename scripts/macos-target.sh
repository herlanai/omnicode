# Shared target settings; source after changing to the repository root.
case "${OMNICODE_ARCH:-$(uname -m)}" in
  arm64) arch=arm64; go_arch=arm64; node_arch=arm64;;
  x86_64|amd64|intel) arch=x86_64; go_arch=amd64; node_arch=x64;;
  *) echo 'OMNICODE_ARCH must be arm64 or x86_64' >&2; exit 1;;
esac
build_dir="$PWD/bin/macos-$arch"
mkdir -p "$build_dir"

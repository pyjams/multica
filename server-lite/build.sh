#!/usr/bin/env bash
# Build server-lite for all platforms.
# Output goes to dist/<platform>/server-lite[.exe]
set -euo pipefail

cd "$(dirname "$0")"

BINARY_NAME="server-lite"

build() {
  local os="$1"
  local arch="$2"
  local ext="${3:-}"
  local out="dist/${os}/${BINARY_NAME}${ext}"

  echo "Building ${out} ..."
  mkdir -p "dist/${os}"
  GOOS="${os}" GOARCH="${arch}" CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    go build -ldflags="-s -w" -trimpath -o "${out}" .
  echo "  -> ${out} ($(du -sh "${out}" | cut -f1))"
}

# Windows x64
build windows amd64 .exe

# macOS arm64 (Apple Silicon)
build darwin arm64

# macOS x64
build darwin amd64

# Linux x64
build linux amd64

echo ""
echo "Done. Artifacts:"
find dist -type f | sort

#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-wjohhan/ffmpeg-benchmark}"
VERSION="${VERSION:-}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="ffmpeg-benchmark"

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux)
    os="linux"
    ;;
  Darwin)
    os="darwin"
    ;;
  *)
    echo "Unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64)
    arch="amd64"
    ;;
  arm64|aarch64)
    arch="arm64"
    ;;
  *)
    echo "Unsupported arch: $arch" >&2
    exit 1
    ;;
esac

if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
fi

if [ -z "$VERSION" ]; then
  echo "Could not determine release version. Set VERSION=vX.Y.Z and retry." >&2
  exit 1
fi

version_no_v="${VERSION#v}"
archive="${BINARY_NAME}_${version_no_v}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading ${url}"
curl -fsSL "$url" -o "$tmpdir/$archive"

tar -xzf "$tmpdir/$archive" -C "$tmpdir"

mkdir -p "$INSTALL_DIR"
cp "$tmpdir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

echo "Installed: $INSTALL_DIR/$BINARY_NAME"
if ! command -v "$BINARY_NAME" >/dev/null 2>&1; then
  echo "Add to PATH if needed: export PATH=\"$INSTALL_DIR:\$PATH\""
fi

"$INSTALL_DIR/$BINARY_NAME" --version

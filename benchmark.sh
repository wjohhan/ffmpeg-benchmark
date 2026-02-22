#!/usr/bin/env bash
set -euo pipefail

REPO="wjohhan/ffmpeg-benchmark"
INSTALL_URL="https://raw.githubusercontent.com/${REPO}/main/install.sh"

if [ -x "./ffmpeg-benchmark" ]; then
  exec ./ffmpeg-benchmark run "$@"
fi

if command -v ffmpeg-benchmark >/dev/null 2>&1; then
  exec ffmpeg-benchmark run "$@"
fi

echo "benchmark.sh is now a compatibility wrapper." >&2
echo "Installing ffmpeg-benchmark binary..." >&2

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for auto-install. Install curl and run:" >&2
  echo "  curl -fsSL ${INSTALL_URL} | bash" >&2
  exit 1
fi

curl -fsSL "$INSTALL_URL" | bash

if command -v ffmpeg-benchmark >/dev/null 2>&1; then
  exec ffmpeg-benchmark run "$@"
fi

if [ -x "$HOME/.local/bin/ffmpeg-benchmark" ]; then
  exec "$HOME/.local/bin/ffmpeg-benchmark" run "$@"
fi

echo "Install finished but ffmpeg-benchmark is not in PATH." >&2
echo "Run with: $HOME/.local/bin/ffmpeg-benchmark run" >&2
exit 1

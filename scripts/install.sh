#!/usr/bin/env bash
# tfreview のリリースバイナリを落として dest に置く。action.yml から呼ぶ。
set -euo pipefail

version="${1:-latest}"
dest="${2:-/usr/local/bin}"
repo="nakamasato/tfreview"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  url="https://github.com/${repo}/releases/latest/download/tfreview_${os}_${arch}.tar.gz"
else
  url="https://github.com/${repo}/releases/download/${version}/tfreview_${os}_${arch}.tar.gz"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -sSfL "$url" -o "$tmp/tfreview.tar.gz"
tar -xzf "$tmp/tfreview.tar.gz" -C "$tmp"
mkdir -p "$dest"
install -m 0755 "$tmp/tfreview" "$dest/tfreview"
echo "installed tfreview to $dest/tfreview"

#!/bin/sh
# packmyrepo installer for macOS and Linux.
#   curl -fsSL https://raw.githubusercontent.com/sahbadal/packmyrepo/main/install.sh | sh
# Options (environment variables):
#   PACKMYREPO_VERSION=v0.1.0       install a specific release (default: latest)
#   PACKMYREPO_INSTALL_DIR=~/bin    where to put the binary (default: ~/.local/bin)
set -eu

REPO="sahbadal/packmyrepo"
NAME="packmyrepo"
INSTALL_DIR="${PACKMYREPO_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${PACKMYREPO_VERSION:-latest}"

say() { printf '%s\n' "$*"; }
die() { printf 'packmyrepo install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported system $(uname -s). On Windows use install.ps1." ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
archive="${NAME}_${os}_${arch}.tar.gz"

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q "$1" -O "$2"; }
else
  die "curl or wget is required"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading $archive ($VERSION)..."
fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "could not download checksums.txt"

# Verify the download before running anything from it.
expected="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$expected" ] || die "no checksum listed for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$expected" = "$actual" ] || die "checksum mismatch, the download may be corrupted. Nothing was installed."

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$INSTALL_DIR"
mv -f "$tmp/$NAME" "$INSTALL_DIR/$NAME"
chmod +x "$INSTALL_DIR/$NAME"

say "Installed $("$INSTALL_DIR/$NAME" version) to $INSTALL_DIR/$NAME"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) say "Run: packmyrepo --help" ;;
  *)
    say ""
    say "$INSTALL_DIR is not on your PATH yet. Add this line to ~/.bashrc or ~/.zshrc:"
    say "  export PATH=\"$INSTALL_DIR:\$PATH\""
    say "then open a new terminal and run: packmyrepo --help"
    ;;
esac
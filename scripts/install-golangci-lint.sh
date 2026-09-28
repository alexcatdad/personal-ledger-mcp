#!/bin/sh
# Install the pinned upstream release without modifying the application's module graph.
set -eu
version=${1:?usage: install-golangci-lint.sh vX.Y.Z}
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo 'Invalid version' >&2; exit 1 ;; esac
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in darwin|linux) ;; *) echo "Unsupported OS: $os" >&2; exit 1 ;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) echo 'Unsupported architecture' >&2; exit 1 ;; esac
dest=".tools/golangci-lint-$version-$os-$arch"
if [ -x "$dest/golangci-lint" ]; then exit 0; fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
release="golangci-lint-${version#v}"
archive="$release-$os-$arch.tar.gz"
base="https://github.com/golangci/golangci-lint/releases/download/$version"
curl --fail --silent --show-error --location "$base/$archive" -o "$work/$archive"
curl --fail --silent --show-error --location "$base/${release}-checksums.txt" -o "$work/checksums"
expected=$(awk -v file="$archive" '$2 == file { print $1 }' "$work/checksums")
[ ${#expected} -eq 64 ] || { echo 'Missing/invalid upstream checksum' >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
 actual=$(sha256sum "$work/$archive" | awk '{print $1}')
else
 actual=$(shasum -a 256 "$work/$archive" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || { echo 'Release checksum mismatch' >&2; exit 1; }
tar -xzf "$work/$archive" -C "$work"
mkdir -p "$dest"
cp "$work/$release-$os-$arch/golangci-lint" "$dest/golangci-lint"
chmod 755 "$dest/golangci-lint"

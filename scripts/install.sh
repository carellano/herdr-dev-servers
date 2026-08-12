#!/bin/sh
set -eu

owner_repo='carellano/herdr-dev-servers'
binary='herdr-dev-servers'

usage() {
  printf '%s\n' "usage: $0 <exact-version>" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
version=$1
[ "$version" = '0.1.2' ] || usage

temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/herdr-dev-servers.XXXXXX")
cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup 0 HUP INT TERM

download() {
  url=$1
  output=$2
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --silent --show-error --retry 2 --output "$output" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$output" "$url"
  else
    printf '%s\n' 'release download unavailable: install curl or wget, or install Go for a local build' >&2
    return 1
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}'
  else
    printf '%s\n' 'cannot verify release archive: install sha256sum, shasum, or openssl' >&2
    return 1
  fi
}

build_local() {
  if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'release download unavailable and Go is not installed; install Go 1.24.0 or newer' >&2
    exit 1
  fi
  required=$(awk '$1 == "go" { print $2; exit }' go.mod)
  have=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
  if [ -z "$required" ] || [ -z "$have" ] || ! awk -v have="$have" -v required="$required" 'BEGIN {
    split(have, h, "."); split(required, r, ".");
    for (i = 1; i <= 3; i++) {
      hv = (i in h) ? h[i] + 0 : 0; rv = (i in r) ? r[i] + 0 : 0;
      if (hv > rv) exit 0; if (hv < rv) exit 1;
    }
    exit 0;
  }'; then
    printf '%s\n' "release download unavailable and Go $required or newer is required (found ${have:-unknown})" >&2
    exit 1
  fi
  printf '%s\n' "building locally with Go $have" >&2
  go build -o "$binary" ./cmd/herdr-dev-servers
}

if [ "${HERDR_DEV_SERVERS_FORCE_BUILD:-}" = '1' ]; then
  build_local
  exit 0
fi

case $(uname -s) in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) printf '%s\n' 'unsupported operating system: only darwin and linux are supported' >&2; exit 1 ;;
esac
case $(uname -m) in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) printf '%s\n' 'unsupported architecture: only amd64 and arm64 are supported' >&2; exit 1 ;;
esac

archive="${binary}_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/${owner_repo}/releases/download/v${version}"
archive_path="$temp_dir/$archive"
checksums_path="$temp_dir/checksums.txt"

if download "$base_url/$archive" "$archive_path" && download "$base_url/checksums.txt" "$checksums_path"; then
  expected=$(awk -v asset="$archive" '$2 == asset || $2 == "*" asset { print $1; exit }' "$checksums_path")
  actual=$(sha256 "$archive_path")
  if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
    printf '%s\n' 'release checksum verification failed' >&2
    exit 1
  fi
  if ! tar -tzf "$archive_path" | grep -Fx "$binary" >/dev/null 2>&1; then
    printf '%s\n' 'release archive does not contain the expected binary' >&2
    exit 1
  fi
  extracted="$temp_dir/$binary"
  tar -xzf "$archive_path" -O "$binary" >"$extracted"
  chmod 755 "$extracted"
  mv "$extracted" "./$binary"
  printf '%s\n' "installed $binary $version" >&2
else
  printf '%s\n' 'release download unavailable; falling back to a local Go build' >&2
  build_local
fi

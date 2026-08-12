#!/bin/sh
set -eu

fail() {
  printf '%s\n' "release contract check failed: $1" >&2
  exit 1
}

require_line() {
  grep -Fqx "$2" "$1" || fail "missing '$2' in $1"
}

require_line go.mod 'module github.com/carellano/herdr-dev-servers'
version=$(awk -F '"' '$1 == "version = " { print $2; exit }' herdr-plugin.toml)
[ "$version" = '0.1.2' ] || fail 'manifest version must be 0.1.2'
require_line herdr-plugin.toml 'id = "carellano.dev-servers"'
require_line herdr-plugin.toml 'command = ["sh", "./scripts/install.sh", "0.1.2"]'
require_line herdr-plugin.toml 'id = "open"'
require_line herdr-plugin.toml 'id = "dev-servers"'
grep -Fq "## $version" CHANGELOG.md || fail "missing $version changelog entry"
grep -Fq 'carellano/herdr-dev-servers' scripts/install.sh || fail 'installer owner/repository is incorrect'
grep -Fq 'archive="${binary}_${version}_${os}_${arch}.tar.gz"' scripts/install.sh || fail 'installer asset template is incorrect'
grep -Fq '{{ .ProjectName }}_{{ .Version }}_{{ .Os | tolower }}_{{ .Arch }}' .goreleaser.yaml || fail 'GoReleaser archive template is incorrect'

publication_files='README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md herdr-plugin.toml scripts/install.sh scripts/check-release.sh .goreleaser.yaml .github/workflows/ci.yml .github/workflows/release.yml'
legacy_product='herdr-'"apps"
legacy_plugin='carellano.'"apps"
legacy_metadata=$(printf '\\%sapps' '$')
legacy_pattern=$(printf '%s|%s|%s' "$legacy_product" "$legacy_plugin" "$legacy_metadata")
if grep -En "$legacy_pattern" $publication_files >/dev/null 2>&1; then
  grep -En "$legacy_pattern" $publication_files >&2 || true
  fail 'old product identifier found in publication files'
fi

printf '%s\n' "release contract valid for $version"

#!/usr/bin/env bash

set -euo pipefail

# Keep archive-content comparisons stable across runner and maintainer locales.
export LC_ALL=C

dist_dir=${1:-dist}

if [[ ! -d "$dist_dir" ]]; then
  echo "::error::release artifact directory does not exist: $dist_dir"
  exit 1
fi

expected_archives=(
  cs2-analyser-tool-darwin-amd64.tar.gz
  cs2-analyser-tool-darwin-arm64.tar.gz
  cs2-analyser-tool-linux-amd64.tar.gz
  cs2-analyser-tool-windows-amd64.zip
)
if ! diff -u \
  <(printf '%s\n' "${expected_archives[@]}") \
  <(for archive in "$dist_dir"/*.tar.gz "$dist_dir"/*.zip; do
      [[ -f "$archive" ]] || continue
      printf '%s\n' "${archive##*/}"
    done | sort); then
  echo "::error::archive set does not match the release contract"
  exit 1
fi

if [[ ! -f "$dist_dir/checksums.txt" ]]; then
  echo "::error::release artifacts do not include checksums.txt"
  exit 1
fi
if ! diff -u \
  <(printf '%s\n' "${expected_archives[@]}") \
  <(awk '{print $2}' "$dist_dir/checksums.txt" | sort); then
  echo "::error::checksums.txt does not cover the release archive set"
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && sha256sum --check checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$dist_dir" && shasum -a 256 --check checksums.txt)
else
  echo "::error::sha256sum or shasum is required to verify checksums.txt"
  exit 1
fi

expected_documentation=(
  LICENSE
  README.md
  _docs/CLI.md
  _docs/DEVELOPMENT.md
  _docs/GO_LIBRARY.md
  _docs/HLTV_REGRESSION.md
  _docs/INSTALLATION.md
  _docs/PLAYER_DATA.MD
  _docs/RATING.MD
)
expected_unix_contents=("${expected_documentation[@]}" cs2-analyser-tool)
for archive in "$dist_dir"/*.tar.gz; do
  if ! diff -u \
    <(printf '%s\n' "${expected_unix_contents[@]}") \
    <(tar -tzf "$archive" | sort); then
    echo "::error::$archive does not contain the expected release files"
    exit 1
  fi
done
if ! diff -u \
  <(printf '%s\n' "${expected_documentation[@]}" cs2-analyser-tool.exe) \
  <(unzip -Z1 "$dist_dir/cs2-analyser-tool-windows-amd64.zip" | sort); then
  echo "::error::Windows archive does not contain the expected release files"
  exit 1
fi

if ! command -v file >/dev/null 2>&1; then
  echo "::error::file is required to inspect release binaries"
  exit 1
fi

assert_binary_format() {
  local platform=$1
  local binary=$2
  local pattern=$3
  local description

  description=$(file -b "$binary")
  if [[ ! "$description" =~ $pattern ]]; then
    echo "::error::$platform binary has unexpected format: $description"
    exit 1
  fi
  printf '%s: %s\n' "$platform" "$description"
}

inspect_dir=$(mktemp -d)
trap 'rm -rf -- "$inspect_dir"' EXIT
mkdir -p \
  "$inspect_dir/linux-amd64" \
  "$inspect_dir/windows-amd64" \
  "$inspect_dir/darwin-amd64" \
  "$inspect_dir/darwin-arm64"
tar -xzf "$dist_dir/cs2-analyser-tool-linux-amd64.tar.gz" -C "$inspect_dir/linux-amd64"
unzip -q "$dist_dir/cs2-analyser-tool-windows-amd64.zip" -d "$inspect_dir/windows-amd64"
tar -xzf "$dist_dir/cs2-analyser-tool-darwin-amd64.tar.gz" -C "$inspect_dir/darwin-amd64"
tar -xzf "$dist_dir/cs2-analyser-tool-darwin-arm64.tar.gz" -C "$inspect_dir/darwin-arm64"

assert_binary_format "Linux AMD64" "$inspect_dir/linux-amd64/cs2-analyser-tool" 'ELF 64-bit.*x86-64'
assert_binary_format "Windows AMD64" "$inspect_dir/windows-amd64/cs2-analyser-tool.exe" 'PE32\+.*x86-64'
assert_binary_format "macOS AMD64" "$inspect_dir/darwin-amd64/cs2-analyser-tool" 'Mach-O 64-bit x86_64'
assert_binary_format "macOS ARM64" "$inspect_dir/darwin-arm64/cs2-analyser-tool" 'Mach-O 64-bit arm64'

linux_binary="$inspect_dir/linux-amd64/cs2-analyser-tool"
binary_name=${linux_binary##*/}
version_output=$("$linux_binary" version)
if [[ -n "${EXPECTED_RELEASE_VERSION:-}" ]]; then
  expected_version_output="$binary_name version $EXPECTED_RELEASE_VERSION"
  if [[ "$version_output" != "$expected_version_output" ]]; then
    echo "::error::release binary reported $version_output, expected $expected_version_output"
    exit 1
  fi
elif [[ ! "$version_output" =~ ^${binary_name}\ version\ v.+$ ]]; then
  echo "::error::snapshot binary reported an invalid version: $version_output"
  exit 1
fi

printf '%s\n' "$version_output"

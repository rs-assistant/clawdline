#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-shell-pairing.XXXXXX")
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
swiftc -target arm64-apple-macos13.0 -o "$test_dir/test" \
  shell/darwin/Pairing.swift shell/darwin/NextConfig.swift tools/test-shell-pairing-permission.swift
# An empty directory of its own: nothing here may read the person's token.
CLAWDLINE_NEXT_DIR="$test_dir/next" "$test_dir/test"

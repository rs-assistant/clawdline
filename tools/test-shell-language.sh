#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-shell-language.XXXXXX")
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
swiftc -target arm64-apple-macos13.0 -o "$test_dir/test" \
  shell/darwin/ShellLanguage.swift tools/test-shell-language.swift
"$test_dir/test" shell/darwin/Resources/shell-copy.json

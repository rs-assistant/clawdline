#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_bin=$(mktemp "${TMPDIR:-/tmp}/staged-app-watcher.XXXXXX")
trap 'rm -f "$test_bin"' EXIT
swiftc -o "$test_bin" shell/darwin/StagedAppWatcher.swift tools/swift-tests/StagedAppWatcherTest.swift
"$test_bin"

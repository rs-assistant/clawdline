#!/bin/bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-machine-delta.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/tools" "$scratch/internal"
cp "$root/tools/check-machine-words.sh" "$root/tools/machine-words.py" "$root/tools/machine-words.allow" "$scratch/tools/"
cd "$scratch"
git init -q
printf 'package example\nvar message = "this machine"\n' > internal/ok.go
printf 'package example\nvar message = "this Mac"\n' > internal/bad.go
git add internal/ok.go internal/bad.go
bash tools/check-machine-words.sh -- ':(literal)internal/ok.go' > /dev/null
if bash tools/check-machine-words.sh -- ':(literal)internal/bad.go' > /dev/null 2>&1; then exit 1; else [ "$?" = 1 ]; fi
cp internal/bad.go internal/new.go
if bash tools/check-machine-words.sh -- ':(literal)internal/new.go' > /dev/null 2>&1; then exit 1; else [ "$?" = 1 ]; fi
rm internal/bad.go
bash tools/check-machine-words.sh -- ':(literal)internal/bad.go' > /dev/null
if bash tools/check-machine-words.sh -- > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
echo 'machine-word delta guard: PASS'

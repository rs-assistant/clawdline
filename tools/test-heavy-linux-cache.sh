#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.." || exit 1

scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir "$scratch/bin"
cat >"$scratch/bin/go" <<'EOF'
#!/bin/bash
printf '%s\n' "$*" >>"$CALLS"
exit "${CLEAN_EXIT:-0}"
EOF
chmod +x "$scratch/bin/go"
export PATH="$scratch/bin:$PATH" CALLS="$scratch/calls"

tools/run-heavy-linux.sh sh -c 'exit 0'
[ "$(cat "$CALLS")" = 'clean -cache -modcache -testcache -fuzzcache' ]

: >"$CALLS"
if tools/run-heavy-linux.sh sh -c 'exit 17'; then exit 1; else code=$?; fi
[ "$code" -eq 17 ]
[ "$(cat "$CALLS")" = 'clean -cache -modcache -testcache -fuzzcache' ]

: >"$CALLS"
export CLEAN_EXIT=19
if tools/run-heavy-linux.sh sh -c 'exit 0'; then exit 1; else code=$?; fi
[ "$code" -eq 19 ]
[ "$(cat "$CALLS")" = 'clean -cache -modcache -testcache -fuzzcache' ]

echo 'heavy Linux cache cleanup: PASS'

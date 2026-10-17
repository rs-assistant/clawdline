#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.." || exit 1
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir "$scratch/bin"
cat > "$scratch/bin/go" <<'MOCK'
#!/bin/bash
printf '%s\n' "$*" >> "$CALLS"
case "$*" in
  clean*) [ "${CLEAN_EXIT:-0}" = 0 ] || exit "$CLEAN_EXIT" ;;
esac
if [ "$*" = 'clean -modcache' ]; then printf 0 > "$USED_FILE"; fi
if [ "$*" = 'clean -cache -testcache -fuzzcache' ]; then
  : > "$GOCACHE/cleared"
  printf 0 > "$USED_FILE"
fi
MOCK
cat > "$scratch/bin/du" <<'MOCK'
#!/bin/bash
[ "${DU_EXIT:-0}" = 0 ] || exit "$DU_EXIT"
[ ! -e "$XDG_CACHE_HOME/fail-measure" ] || exit 23
if [ -e "$XDG_CACHE_HOME/trim-canary" ] && [ ! -e "$XDG_CACHE_HOME/clawdline/heavy-go-v1/build/ab/$(printf '%064d' 0)-d" ]; then printf 0 > "$USED_FILE"; fi
printf '%s\tcache\n' "$(cat "$USED_FILE")"
MOCK
cat > "$scratch/bin/df" <<'MOCK'
#!/bin/bash
printf 'Filesystem 1024-blocks Used Available Capacity Mounted\nfixture 30000000 1 %s 1%% /\n' "${FREE_KIB:-10000000}"
MOCK
chmod +x "$scratch/bin/"*
export PATH="$scratch/bin:$PATH" CALLS="$scratch/calls" USED_FILE="$scratch/used" XDG_CACHE_HOME="$scratch/cache"
unset GOCACHE GOMODCACHE CLAWDLINE_HEAVY_GO_CACHE_ACTIVE
printf 1 > "$USED_FILE"
: > "$CALLS"
run=tools/run-heavy-linux.sh
"$run" sh -c 'printf retained > "$GOCACHE/proof"; printf module > "$GOMODCACHE/proof"'
[ ! -s "$CALLS" ]
"$run" sh -c 'test "$(cat "$GOCACHE/proof")" = retained; test "$(cat "$GOMODCACHE/proof")" = module'
[ ! -s "$CALLS" ]
# Read the bound from its production source; no duplicated test ceiling.
limit=$(sed -n 's/^const MaxHeavyGoCacheKiB = \([0-9][0-9]*\)$/\1/p' internal/domain/capacity/heavy_cache.go)
mkdir -p "$XDG_CACHE_HOME/clawdline/heavy-go-v1/build/ab"
old="$XDG_CACHE_HOME/clawdline/heavy-go-v1/build/ab/$(printf '%064d' 0)-d"
recent="$XDG_CACHE_HOME/clawdline/heavy-go-v1/build/ab/$(printf '%064d' 1)-d"
printf old > "$old"
printf recent > "$recent"
touch -d '2 hours ago' "$old"
touch "$XDG_CACHE_HOME/trim-canary"
printf '%s' "$((limit + 1))" > "$USED_FILE"
"$run" true
[ ! -s "$CALLS" ]
[ ! -e "$old" ]
[ "$(cat "$recent")" = recent ]
[ "$(cat "$XDG_CACHE_HOME/clawdline/heavy-go-v1/build/proof")" = retained ]
rm "$XDG_CACHE_HOME/trim-canary"
[ -f "$XDG_CACHE_HOME/clawdline/heavy-go-v1/mod/proof" ]
# Cleanup is conditional after a failed build and preserves its failure.
: > "$CALLS"
if "$run" sh -c 'printf 999999999 > "$USED_FILE"; exit 17'; then exit 1; else [ "$?" = 17 ]; fi
[ -s "$CALLS" ]
: > "$CALLS"
export CLEAN_EXIT=19
if "$run" sh -c 'printf 999999999 > "$USED_FILE"'; then exit 1; else [ "$?" = 19 ]; fi
printf 1 > "$USED_FILE"
if "$run" sh -c 'printf 999999999 > "$USED_FILE"; exit 17'; then exit 1; else [ "$?" = 17 ]; fi
unset CLEAN_EXIT
printf 1 > "$USED_FILE"
: > "$CALLS"
# Explicit user/broker caches are neither claimed nor cleaned.
GOCACHE="$scratch/user-build" GOMODCACHE="$scratch/gate-mod" "$run" true
[ ! -s "$CALLS" ]
# A mixed environment only permits cleaning the wrapper-owned half.
GOCACHE="$scratch/user-build" "$run" sh -c 'printf 999999999 > "$USED_FILE"'
[ "$(cat "$CALLS")" = 'clean -modcache' ]
printf 1 > "$USED_FILE"
: > "$CALLS"
# Low disk pressure reclaims only owned data, then refuses if still too low.
if FREE_KIB=1 "$run" sh -c 'touch "$XDG_CACHE_HOME/should-not-run"'; then exit 1; else [ "$?" = 73 ]; fi
[ ! -e "$XDG_CACHE_HOME/should-not-run" ]
# Unknown measurements never authorize cleanup or starting a new command.
: > "$CALLS"
if DU_EXIT=23 "$run" sh -c 'touch "$XDG_CACHE_HOME/should-not-run"'; then exit 1; else [ "$?" = 2 ]; fi
[ ! -e "$XDG_CACHE_HOME/should-not-run" ]
[ ! -s "$CALLS" ]
if "$run" sh -c 'touch "$XDG_CACHE_HOME/fail-measure"; exit 17'; then exit 1; else [ "$?" = 17 ]; fi
[ ! -s "$CALLS" ]
rm -- "$XDG_CACHE_HOME/fail-measure"
if "$run" sh -c 'touch "$XDG_CACHE_HOME/fail-measure"'; then exit 1; else [ "$?" = 2 ]; fi
[ ! -s "$CALLS" ]
rm -- "$XDG_CACHE_HOME/fail-measure"
# Nested wrappers do not reacquire their own lock or trim in-flight caches.
: > "$CALLS"
"$run" "$run" true
[ ! -s "$CALLS" ]
# A cache root without an ownership receipt is kept, never adopted.
mkdir -p "$scratch/unknown/clawdline/heavy-go-v1"
printf keep > "$scratch/unknown/clawdline/heavy-go-v1/proof"
if XDG_CACHE_HOME="$scratch/unknown" "$run" true; then exit 1; else [ "$?" = 2 ]; fi
[ "$(cat "$scratch/unknown/clawdline/heavy-go-v1/proof")" = keep ]
# Symlinked roots and cache directories cannot redirect cleanup.
mkdir -p "$scratch/linked/clawdline" "$scratch/elsewhere"
ln -s "$scratch/elsewhere" "$scratch/linked/clawdline/heavy-go-v1"
if XDG_CACHE_HOME="$scratch/linked" "$run" true; then exit 1; else [ "$?" = 2 ]; fi
mv "$XDG_CACHE_HOME/clawdline/heavy-go-v1/build" "$scratch/saved-build"
ln -s "$scratch/elsewhere" "$XDG_CACHE_HOME/clawdline/heavy-go-v1/build"
if "$run" true; then exit 1; else [ "$?" = 2 ]; fi
echo 'heavy Linux managed cache: PASS'

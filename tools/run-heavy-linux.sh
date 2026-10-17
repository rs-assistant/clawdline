#!/bin/bash
# Linux builds share a wrapper-owned cache across worktrees. Never reclaim
# the user's default cache, an explicit cache, or a broker gate's scratch.
set -uo pipefail
[ "$#" -gt 0 ] || { echo 'usage: tools/run-heavy-linux.sh <command> [args...]' >&2; exit 2; }
if [ "${CLAWDLINE_HEAVY_GO_CACHE_ACTIVE:-}" = 1 ] || ! command -v go >/dev/null 2>&1; then
  exec "$@"
fi
if [ -n "${GOCACHE:-}" ] && [ -n "${GOMODCACHE:-}" ]; then exec "$@"; fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P) || exit 2
policy="$script_dir/../internal/domain/capacity/heavy_cache.go"
cache_limit=$(sed -n 's/^const MaxHeavyGoCacheKiB = \([0-9][0-9]*\)$/\1/p' "$policy")
reserve=$(sed -n 's/^const MaxHeavyGoDiskReserveKiB = \([0-9][0-9]*\)$/\1/p' "$policy")
case "$cache_limit:$reserve" in *[!0-9:]*|:*|*:) echo 'heavy_go_cache_policy_invalid' >&2; exit 2 ;; esac

root="${XDG_CACHE_HOME:-$HOME/.cache}/clawdline/heavy-go-v1"
mkdir -p -- "$(dirname -- "$root")" || exit 2
if (umask 077; mkdir -- "$root") 2>/dev/null; then
  printf '%s\n' clawdline-heavy-go-v1 > "$root/owner" || exit 2
fi
if [ -L "$root" ] || [ ! -d "$root" ] || [ "$(stat -c %u -- "$root")" != "$(id -u)" ] ||
   [ "$(stat -c %a -- "$root")" != 700 ] ||
   [ -L "$root/owner" ] || [ ! -f "$root/owner" ] || [ "$(cat "$root/owner" 2>/dev/null)" != clawdline-heavy-go-v1 ]; then
  echo 'heavy_go_cache_owner_unknown: refusing to use or clear this cache' >&2; exit 2
fi
# Also serialize cache use without a daemon. The lock stays outside both Go
# cache directories, so Go's cleanup cannot replace the lock inode.
[ ! -L "$root/lock" ] || { echo 'heavy_go_cache_lock_unsafe' >&2; exit 2; }
exec 9>"$root/lock"
flock -x 9 || exit 2
owned_build=no owned_mod=no
if [ -z "${GOCACHE:-}" ]; then export GOCACHE="$root/build"; owned_build=yes; fi
if [ -z "${GOMODCACHE:-}" ]; then export GOMODCACHE="$root/mod"; owned_mod=yes; fi
owned=()
[ "$owned_build" = no ] || owned+=("$GOCACHE")
[ "$owned_mod" = no ] || owned+=("$GOMODCACHE")
for dir in ${owned[@]+"${owned[@]}"}; do
  [ ! -L "$dir" ] || { echo 'heavy_go_cache_path_unsafe' >&2; exit 2; }
  mkdir -p -- "$dir" || exit 2
done
export CLAWDLINE_HEAVY_GO_CACHE_ACTIVE=1

measure() {
  local sizes
  sizes=$(du -sk -- ${owned[@]+"${owned[@]}"}) || return 1
  used=$(printf '%s\n' "$sizes" | awk '{sum += $1} END {printf "%.0f", sum}')
  free=$(df -Pk -- "$root" | awk 'NR == 2 {print $4}') || return 1
  case "$used:$free" in *[!0-9:]*|:*|*:) return 1 ;; esac
}
# Go refreshes cache entry mtimes on use. Sort metadata on disk rather than
# retaining an unbounded candidate list, and remove only recognized Go entries.
trim_compiled() {
  local manifest record rest file blocks need freed
  need=$(( (used > cache_limit ? used - cache_limit : 0) * 1024 ))
  if [ "$free" -lt "$reserve" ] && [ "$(((reserve - free) * 1024))" -gt "$need" ]; then need=$(((reserve - free) * 1024)); fi
  manifest=$(mktemp "$root/.reclaim.XXXXXX") || return 2
  if ! find "$GOCACHE" -mindepth 2 -maxdepth 2 -type f -printf '%T@ %b %p\0' | sort -z -n > "$manifest"; then
    rm -f -- "$manifest"; return 2
  fi
  freed=0
  while IFS= read -r -d '' record; do
    rest=${record#* }; blocks=${rest%% *}; file=${rest#* }
    [[ "$file" =~ /[0-9a-f]{2}/[0-9a-f]{64}-[ad]$ ]] || continue
    case "$blocks" in ''|*[!0-9]*) rm -f -- "$manifest"; return 2 ;; esac
    rm -- "$file" || { rm -f -- "$manifest"; return 2; }
    freed=$((freed + blocks * 512))
    [ "$freed" -lt "$need" ] || break
  done < "$manifest"
  rm -f -- "$manifest"
}

clear_owned() {
  echo "heavy_go_cache_reclaim: used_kib=$used free_kib=$free budget_kib=$cache_limit reserve_kib=$reserve" >&2
  if [ "$owned_build" = yes ]; then trim_compiled || return $?; fi
  measure || return 2
  # Keep downloaded modules when trimming older compiled entries was sufficient.
  if [ "$owned_mod" = yes ] && { [ "$used" -gt "$cache_limit" ] || [ "$free" -lt "$reserve" ]; }; then
    go clean -modcache || return $?
    mkdir -p -- "$GOMODCACHE" || return 2
  fi
  measure || return 2
  [ "$used" -le "$cache_limit" ] || { echo 'heavy_go_cache_still_over_budget' >&2; return 2; }
}
measure || { echo 'heavy_go_cache_measurement_failed' >&2; exit 2; }
if [ "$used" -gt "$cache_limit" ] || [ "$free" -lt "$reserve" ]; then
  clear_owned || { echo 'heavy_go_cache_reclaim_failed' >&2; exit 2; }
fi
if [ "$free" -lt "$reserve" ]; then
  echo "heavy_go_disk_reserve_unavailable: free_kib=$free reserve_kib=$reserve" >&2; exit 73
fi
"$@"
status=$?
if ! measure; then
  echo 'heavy_go_cache_measurement_failed' >&2
  [ "$status" -ne 0 ] || status=2
elif [ "$used" -gt "$cache_limit" ] || [ "$free" -lt "$reserve" ]; then
  clear_owned
  clean_status=$?
  if [ "$clean_status" -ne 0 ]; then
    echo "heavy_go_cache_reclaim_failed: exit=$clean_status" >&2
    [ "$status" -ne 0 ] || status=$clean_status
  fi
fi
exit "$status"

#!/bin/bash
# Run inside the heavy slot so another managed build cannot start while Go's
# shared caches are being cleared. Keep the command's failure if both fail.
set -u

"$@"
status=$?
if command -v go >/dev/null 2>&1; then
  go clean -cache -modcache -testcache -fuzzcache
  clean_status=$?
  if [ "$clean_status" -ne 0 ]; then
    echo "tools/heavy.sh: could not clear all Go caches (go clean exited $clean_status)" >&2
    if [ "$status" -eq 0 ]; then status=$clean_status; fi
  fi
fi
exit "$status"

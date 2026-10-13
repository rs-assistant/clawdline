#!/bin/bash
# Run every working-tree privacy rule in CI without pretending that CI owns
# the private word list. Exit 3 is preserved as a visible warning. Historical
# scans are explicit publication audits, not an automatic CI gate.
set -uo pipefail
cd "$(dirname "$0")/.."

tools/check-private.sh
tree_status=$?
case "$tree_status" in
  0) echo "privacy: working tree complete" ;;
  3)
    echo "::warning title=Privacy guard undetermined::The working tree passed the fixed rules, but CI has no private-word list. This is not publication approval."
    ;;
  *) echo "privacy: working tree failed with exit $tree_status" >&2; exit "$tree_status" ;;
esac

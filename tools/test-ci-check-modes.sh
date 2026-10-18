#!/bin/sh
# The legacy check answers from the pinned manifest, with or without a source.
#
# It used to need ~/code/clawdline present and exit 2 without it, and CI passed
# --allow-missing-source, which skipped the check entirely — so the lock was
# never verified anywhere. It is verified everywhere now, and this holds what
# matters: a missing source does not stop the check, a changed stylesheet is
# caught whether or not a source is there to compare against, the JavaScript
# beside it is owned code that may change, and a manifest that pins it again is
# refused rather than half-checked.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-ci-modes.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
missing="$tmp/no-legacy-source"

# 1. No source: the copies are still checked, and the absence is named rather
#    than passed over in silence.
set +e
CLAWDLINE_WEB_SOURCE_TREE="$missing" "$root/tools/check-legacy-css.sh" >"$tmp/nosrc.out" 2>&1
nosrc=$?
set -e
if [ "$nosrc" -ne 0 ]; then
  echo "a missing source returned $nosrc, want 0" >&2; cat "$tmp/nosrc.out" >&2; exit 1
fi
if ! grep -Fq 'source unavailable (optional)' "$tmp/nosrc.out"; then
  echo "a missing source was not named:" >&2; cat "$tmp/nosrc.out" >&2; exit 1
fi
if ! grep -Fq 'copies match the pinned manifest' "$tmp/nosrc.out"; then
  echo "the copies were not checked without a source:" >&2; cat "$tmp/nosrc.out" >&2; exit 1
fi

# 2. The old spelling still works, for a CI job that has not been updated yet.
CLAWDLINE_WEB_SOURCE_TREE="$missing" "$root/tools/check-legacy-css.sh" \
  --allow-missing-source >"$tmp/flag.out" 2>&1
if ! grep -Fq 'copies match the pinned manifest' "$tmp/flag.out"; then
  echo "--allow-missing-source stopped the check:" >&2; cat "$tmp/flag.out" >&2; exit 1
fi

# 3. A changed copy is caught with no source present. Nothing in the repository
#    is edited: the check runs against a copy of the tree.
work="$tmp/tree"
mkdir -p "$work/tools" "$work/web/console/src/legacy" "$work/web/console/public/strings"
cp -R "$root/web/console/src/legacy/." "$work/web/console/src/legacy/"
cp -R "$root/web/console/public/strings/." "$work/web/console/public/strings/"
cp "$root/tools/check-legacy-css.sh" "$work/tools/"
printf '\n' >>"$work/web/console/src/legacy/tokens.css"
set +e
( cd "$work" && CLAWDLINE_WEB_SOURCE_TREE="$missing" tools/check-legacy-css.sh ) >"$tmp/drift.out" 2>&1
drift=$?
set -e
if [ "$drift" -ne 1 ]; then
  echo "an edited copy returned $drift, want 1" >&2; cat "$tmp/drift.out" >&2; exit 1
fi
if ! grep -Fq 'copy changed' "$tmp/drift.out"; then
  echo "an edited copy was not named:" >&2; cat "$tmp/drift.out" >&2; exit 1
fi

# 4. The JavaScript is not a copy: editing it is not drift.
cp "$root/web/console/src/legacy/tokens.css" "$work/web/console/src/legacy/tokens.css"
printf '\n' >>"$work/web/console/src/legacy/js/core/esc.js"
set +e
( cd "$work" && CLAWDLINE_WEB_SOURCE_TREE="$missing" tools/check-legacy-css.sh ) >"$tmp/owned.out" 2>&1
owned=$?
set -e
if [ "$owned" -ne 0 ]; then
  echo "an edited JavaScript file returned $owned, want 0" >&2; cat "$tmp/owned.out" >&2; exit 1
fi

# 5. A manifest that pins JavaScript again cannot be checked as one: it is refused.
python3 - "$work/web/console/src/legacy/MANIFEST.json" <<'PY'
import json, sys
path = sys.argv[1]
with open(path, encoding="utf-8") as source:
    manifest = json.load(source)
manifest["js"] = {"js/core/esc.js": "0000000000000000"}
with open(path, "w", encoding="utf-8") as target:
    json.dump(manifest, target)
PY
set +e
( cd "$work" && CLAWDLINE_WEB_SOURCE_TREE="$missing" tools/check-legacy-css.sh ) >"$tmp/pinned.out" 2>&1
pinned=$?
set -e
if [ "$pinned" -ne 2 ]; then
  echo "a manifest pinning JavaScript returned $pinned, want 2" >&2; cat "$tmp/pinned.out" >&2; exit 1
fi
if ! grep -Fq 'owned code, not a copy' "$tmp/pinned.out"; then
  echo "a manifest pinning JavaScript was not named:" >&2; cat "$tmp/pinned.out" >&2; exit 1
fi

echo "CI check modes: the pinned stylesheets are checked with or without a source, an edited one is caught either way, and the JavaScript is owned code"

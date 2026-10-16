#!/bin/bash
# Recapture the website's three feature pictures from the built console and the
# story in web/console/src/session/site-shots.ts, in English and Taiwan
# Traditional Chinese, with their numbered callouts drawn in. docs/site-shots.md
# is the whole of it.
#
#   tools/site-shots.sh <directory>
#
# Writes attention-note-*.png, session-todo-*.png and board-item-*.png (each in
# -en and -zh-Hant) into <directory>, normally the website's public/features.
# Builds the console first through tools/heavy.sh. Exit: 0 when all six were
# written, 1 otherwise, 2 for a usage error.
set -euo pipefail

if [ $# -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: tools/site-shots.sh <existing output directory>" >&2
  exit 2
fi
out=$(cd "$1" && pwd)
root=$(cd "$(dirname "$0")/.." && pwd)

( cd "$root/web" && "$root/tools/heavy.sh" npm run build )

scenes=(attention-note session-todo board-item)
for lang in en zh-Hant; do
  for scene in ${scenes[@]+"${scenes[@]}"}; do rm -f "$out/$scene-$lang.png"; done
  ( cd "$root/web/console" && SITE_SHOTS="$out" SITE_SHOTS_LANG="$lang" \
      node --test --experimental-strip-types src/session/site-shots.e2e.ts )
done

missing=0
for lang in en zh-Hant; do
  for scene in ${scenes[@]+"${scenes[@]}"}; do
    if [ ! -s "$out/$scene-$lang.png" ]; then
      echo "site-shots: $scene-$lang.png was not written" >&2
      missing=1
    fi
  done
done
[ "$missing" -eq 0 ] || exit 1
echo "site-shots: six pictures in $out; look at every one before publishing them"

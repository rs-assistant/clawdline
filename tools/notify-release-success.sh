#!/bin/sh
# Notify once for a verified Cloud or app deployment on this machine.
# A failed or unavailable push never turns a successful deployment into a failure.
set -u

[ "$#" -eq 2 ] || { echo "usage: $0 cloud|app <commit>" >&2; exit 2; }
kind=$1
stamp=$2
case "$kind" in cloud|app) ;; *) echo "unknown release kind: $kind" >&2; exit 2 ;; esac
case "$stamp" in *[!0-9a-f]*|'') echo "invalid release stamp" >&2; exit 2 ;; esac

state=${CLAWDLINE_NEXT_DIR:-"$HOME/.config/clawdline-next"}/release-notices
mkdir -p "$state" || { echo "release notice: state unavailable" >&2; exit 0; }
receipt="$state/$kind-$stamp.sent"
[ ! -e "$receipt" ] || exit 0
lock="$state/$kind-$stamp.lock"
mkdir "$lock" 2>/dev/null || exit 0
trap 'rmdir "$lock" 2>/dev/null || true' EXIT HUP INT TERM
[ ! -e "$receipt" ] || exit 0

if [ "$kind" = cloud ]; then
  title='Cloud release is live'
  body="app.clawdline.com is serving $stamp and passed its production checks."
  title_zh='Cloud 新版本已上線'
  body_zh="app.clawdline.com 已提供 ${stamp}，正式環境檢查通過。"
else
  title='App update is running'
  body="The app is running $stamp and passed its local checks."
  title_zh='App 新版本已啟動'
  body_zh="App 已執行 ${stamp}，本機檢查通過。"
fi

if [ -n "${CLAWDLINE_RELEASE_NOTIFY_BIN:-}" ]; then
  notifier=$CLAWDLINE_RELEASE_NOTIFY_BIN
else
  notifier=clawdline
fi
language=$("$notifier" setting get product_language 2>/dev/null || true)
enabled=$("$notifier" setting get "push_on_${kind}_release_success" 2>/dev/null || true)
case "$enabled" in *': on'|*': off') ;; *) enabled=$("$notifier" setting get push_on_deploy 2>/dev/null || true) ;; esac
case "$enabled" in *': on') ;; *) exit 0 ;; esac
case "$language" in *zh-Hant*) title=$title_zh; body=$body_zh ;; esac
if "$notifier" notify --title "$title" --body "$body"; then
  : > "$receipt"
else
  echo "release notice: $kind $stamp was verified, but push was not delivered" >&2
fi
exit 0

#!/bin/sh
# Notify once when a release command fails after it began checking a release.
# Push failure never changes the command's original exit status.
set -u

[ "$#" -eq 2 ] || exit 2
kind=$1
stamp=$2
case "$kind" in cloud|app) ;; *) exit 2 ;; esac
case "$stamp" in *[!0-9a-f]*|'') exit 2 ;; esac

state=${CLAWDLINE_NEXT_DIR:-"$HOME/.config/clawdline-next"}/release-notices
mkdir -p "$state" || exit 0
[ ! -e "$state/$kind-$stamp.sent" ] || exit 0
receipt="$state/failure-$kind-$stamp.sent"
[ ! -e "$receipt" ] || exit 0
lock="$state/failure-$kind-$stamp.lock"
mkdir "$lock" 2>/dev/null || exit 0
trap 'rmdir "$lock" 2>/dev/null || true' EXIT HUP INT TERM
[ ! -e "$receipt" ] || exit 0

notifier=${CLAWDLINE_RELEASE_NOTIFY_BIN:-clawdline}
[ -x "$notifier" ] || notifier=clawdline
enabled=$("$notifier" setting get push_on_release_failure 2>/dev/null || true)
case "$enabled" in *': on'|*': off') ;; *) enabled=$("$notifier" setting get push_on_deploy 2>/dev/null || true) ;; esac
case "$enabled" in *': on') ;; *) exit 0 ;; esac

if [ "$kind" = cloud ]; then
  title='Cloud release needs attention'
  title_zh='Cloud 發布未通過驗收'
  body="The production check for $stamp failed. Review the release command output."
  body_zh="${stamp} 的正式環境驗收失敗，請查看發布指令的輸出。"
else
  title='App release needs attention'
  title_zh='App 發布未完成'
  body="The app install or verification for $stamp failed. Review the release command output."
  body_zh="${stamp} 的 App 安裝或驗收失敗，請查看發布指令的輸出。"
fi
language=$("$notifier" setting get product_language 2>/dev/null || true)
case "$language" in *zh-Hant*) title=$title_zh; body=$body_zh ;; esac
if "$notifier" notify --title "$title" --body "$body"; then
  : > "$receipt"
else
  echo "release notice: $kind $stamp failed, but push was not delivered" >&2
fi
exit 0

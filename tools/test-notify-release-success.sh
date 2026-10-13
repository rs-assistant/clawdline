#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
cat > "$tmp/notifier" <<'SH'
#!/bin/sh
case "$*" in
  'setting get product_language') echo 'product_language: zh-Hant' ;;
  'setting get push_on_deploy') echo "push_on_deploy: ${NOTICE_ENABLED:-off}" ;;
  notify*)
    [ "${NOTICE_FAIL:-0}" = 0 ] || exit 1
    printf '%s\n' "$*" >> "$NOTICE_LOG" ;;
  *) exit 2 ;;
esac
SH
chmod +x "$tmp/notifier"
export CLAWDLINE_NEXT_DIR="$tmp/state" CLAWDLINE_RELEASE_NOTIFY_BIN="$tmp/notifier" NOTICE_LOG="$tmp/log"
stamp=abcdef1234567890
tools/notify-release-success.sh cloud "$stamp"
[ ! -e "$tmp/log" ]
export NOTICE_ENABLED=on
tools/notify-release-success.sh cloud "$stamp"
tools/notify-release-success.sh cloud "$stamp"
tools/notify-release-success.sh app "$stamp"
[ "$(wc -l < "$tmp/log" | tr -d ' ')" = 2 ]
grep -q 'Cloud 新版本已上線' "$tmp/log"
grep -q 'App 新版本已啟動' "$tmp/log"
export NOTICE_FAIL=1
tools/notify-release-success.sh cloud 1234567
[ ! -e "$tmp/state/release-notices/cloud-1234567.sent" ]
export NOTICE_FAIL=0
tools/notify-release-success.sh cloud 1234567
[ -e "$tmp/state/release-notices/cloud-1234567.sent" ]
echo 'release notices: PASS'

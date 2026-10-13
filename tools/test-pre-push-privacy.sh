#!/bin/sh
# Prove that the pre-push hook checks the working tree once and never starts a
# historical privacy scan. Every working-tree answer other than clean still
# refuses the push.
#
# The checker itself is stubbed: its rules have their own tests
# (tools/check-private), and what is tested here is the hook's question and
# what it does with the answer. The stub records every call and fails if the
# removed historical mode is invoked.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-push-privacy.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

mkdir -p "$tmp/bin"
cat >"$tmp/bin/gh" <<STUB
#!/bin/sh
case "\$1 \$2" in
  "run list") echo '201 success https://example.invalid/runs/201' ;;
  "run view") : ;;
  *) echo "gh stub: unexpected \$*" >&2; exit 1 ;;
esac
STUB
chmod +x "$tmp/bin/gh"

git init --bare -q "$tmp/public.git"
git init -q -b main "$tmp/work"
mkdir -p "$tmp/work/tools/git-hooks"
cp "$root/tools/git-hooks/pre-push" "$tmp/work/tools/git-hooks/pre-push"
# The stub: every call is appended to calls.txt, answer.txt (when it exists)
# is the working-tree answer, and any historical call is an immediate error.
cat >"$tmp/work/tools/check-private.sh" <<STUB
#!/bin/sh
printf '%s\n' "\${*:-working-tree}" >>"$tmp/calls.txt"
if [ "\${1:-}" = -history ]; then
  echo "historical privacy scan was not disabled" >&2
  exit 99
fi
if [ -f "$tmp/answer.txt" ]; then
  echo "docs/note.md:3: private-word — still in the working tree"
  exit "\$(cat "$tmp/answer.txt")"
fi
exit 0
STUB
chmod +x "$tmp/work/tools/git-hooks/pre-push" "$tmp/work/tools/check-private.sh"
git -C "$tmp/work" config core.hooksPath tools/git-hooks
git -C "$tmp/work" remote add origin "$tmp/public.git"

n=0
commit() {
  n=$((n + 1))
  printf '%s\n' "$n" >"$tmp/work/file.txt"
  git -C "$tmp/work" add file.txt
  git -C "$tmp/work" -c user.name='Guard Test' -c user.email='guard@example.invalid' \
    commit -q -m "Change $n"
}
fail() { echo "push privacy guard: $1" >&2; sed -n '1,30p' "$tmp/push.out" >&2; exit 1; }
push() {
  want=$1
  : >"$tmp/calls.txt"
  if PATH="$tmp/bin:$PATH" git -C "$tmp/work" push -q origin HEAD:refs/heads/main >"$tmp/push.out" 2>&1
  then got=ok; else got=refused; fi
  pushed=$(git --git-dir="$tmp/public.git" rev-parse -q --verify refs/heads/main || true)
  head=$(git -C "$tmp/work" rev-parse HEAD)
  [ "$got" = "$want" ] || fail "push was $got, want $want"
  if [ "$want" = ok ] && [ "$pushed" != "$head" ]; then fail "the push succeeded but main is not HEAD"; fi
  if [ "$want" = refused ] && [ "$pushed" = "$head" ]; then fail "a refused push still moved main"; fi
}
expect() { grep -Fq "$1" "$tmp/push.out" || fail "the output does not say: $1"; }
calls() { grep -c . "$tmp/calls.txt" || true; }

# 1. The first push checks the working tree once and never asks for history.
commit
push ok
[ "$(calls)" = 1 ] || fail "the hook made $(calls) check-private call(s), want one"
grep -Fxq -- "working-tree" "$tmp/calls.txt" ||
  fail "the first push made an unexpected privacy call: $(cat "$tmp/calls.txt")"

# 2. A later push has the same one working-tree check, not a range scan.
commit
commit
push ok
[ "$(calls)" = 1 ] || fail "the hook made $(calls) check-private call(s), want one"
grep -Fxq -- "working-tree" "$tmp/calls.txt" ||
  fail "the later push made an unexpected privacy call: $(cat "$tmp/calls.txt")"

# 3. A working-tree finding refuses the push and names the finding.
echo 1 >"$tmp/answer.txt"
commit
push refused
expect 'the working tree is not clean of private material'
expect 'docs/note.md:3: private-word'

# 4. Could-not-check and undetermined working-tree answers are not a pass.
echo 2 >"$tmp/answer.txt"
push refused
expect 'working tree is not clean of private material'
echo 3 >"$tmp/answer.txt"
push refused
expect 'working tree is not clean of private material'

# 5. Clean again: it lands.
rm "$tmp/answer.txt"
push ok

echo "push privacy guard: a push checks the working tree once, never scans history, and any answer but clean refuses it"

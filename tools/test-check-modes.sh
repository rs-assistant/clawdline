#!/bin/bash
# Exercise the real check entrypoint against a small Git fixture and fake
# compilers. A skipped build must not silently become skipped Go tests.
set -euo pipefail
cd "$(dirname "$0")/.." || exit 1
real_node=$(command -v node)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/repo/tools" "$scratch/repo/web/console/tools" "$scratch/bin"
cp tools/check.sh tools/check-plan.mjs tools/check-scope.mjs tools/check-profile.mjs "$scratch/repo/tools/"
for name in check-legacy-css.sh check-machine-words.sh check-shell-expansions.sh check-private.sh test-notify-release-success.sh test-heavy-linux-cache.sh test-linux-dev-build.sh test-machine-words-changes.sh test-check-modes.sh test-staged-app-watcher.sh; do
  printf '#!/bin/sh\nexit 0\n' > "$scratch/repo/tools/$name"
  chmod +x "$scratch/repo/tools/$name"
done
cat > "$scratch/bin/go" <<'MOCK'
#!/bin/bash
printf 'go %s\n' "$*" >> "$CALLS"
if [ "$1" = list ]; then root=$(pwd -P); printf '{"Dir":"%s/internal/example","ImportPath":"fixture/example"}\n{"Dir":"%s/internal/caller","ImportPath":"fixture/caller","Imports":["fixture/example"]}\n' "$root" "$root"; exit 0; fi
if [ "${FAIL_TESTS:-0}" = 1 ] && [ "$1" = test ]; then exit 7; fi
if [ "$1 $2" = 'test -json' ]; then
  printf '%s\n' '{"Action":"pass","Package":"fixture","Elapsed":1}'
fi
MOCK
cat > "$scratch/bin/node" <<'MOCK'
#!/bin/bash
case "$1" in
  tools/ensure-web-deps.mjs)
    [ "${FAIL_DEPS:-0}" != 1 ] || exit 9
    : > "$CALLS.deps"
    exit 0 ;;
  tools/check-plan.mjs|tools/check-scope.mjs)
    [ -f "$CALLS.deps" ] || exit 31
    if [ "${BROKEN_PLAN:-0}" = 1 ]; then printf 'invalid\n'; exit 0; fi
    exec "$REAL_NODE" "$@" ;;
  tools/check-profile.mjs) exec "$REAL_NODE" "$@" ;;
esac
exit 0
MOCK
cat > "$scratch/bin/npm" <<'MOCK'
#!/bin/bash
printf 'npm %s\n' "$*" >> "$CALLS"
MOCK
printf '#!/bin/sh\nexit 0\n' > "$scratch/bin/gofmt"
chmod +x "$scratch/bin/"*
export PATH="$scratch/bin:$PATH" TMPDIR="$scratch" CALLS="$scratch/calls" REAL_NODE="$real_node" CLAWDLINE_CHECK_WRAPPED=1
unset CLAWDLINE_HEAVY
cd "$scratch/repo"
git init -q -b main
git config user.name Test
git config user.email test@example.invalid
mkdir docs
printf '# Fixture\n' > docs/fixture.md
git add .
git commit -qm 'Initial fixture'
git checkout -qb task
printf 'Documentation edit\n' >> docs/fixture.md
: > "$CALLS"
bash tools/check.sh > "$scratch/docs.log"
! grep -Fxq 'go test ./...' "$CALLS"
grep -Fxq 'go test -count=1 ./internal/config' "$CALLS"
! grep -q '^go build\|^go vet\|^npm' "$CALLS"
mkdir -p internal/example
printf 'package example\n' > internal/example/example.go
: > "$CALLS"
bash tools/check.sh > "$scratch/module.log"
grep -Fxq 'go test fixture/example' "$CALLS"
! grep -Fxq 'go test ./...' "$CALLS"
! grep -Fxq 'go test -run=^$ fixture/caller' "$CALLS"
: > "$CALLS"
bash tools/check.sh --callers > "$scratch/callers.log"
grep -Fxq 'go test -run=^$ fixture/caller' "$CALLS"
rm -- internal/example/example.go
rmdir internal/example internal
: > "$CALLS"
bash tools/check.sh --full > "$scratch/full.log"
grep -Fxq 'go vet ./...' "$CALLS"
grep -Fxq 'go build -o /dev/null ./cmd/clawdline' "$CALLS"
grep -Fxq 'npm run build' "$CALLS"
: > "$CALLS"
bash tools/check.sh --quick ./internal/example > "$scratch/quick.log"
[ "$(cat "$CALLS")" = 'go test ./internal/example' ]
grep -q 'DEVELOPMENT CHECK ONLY' "$scratch/quick.log"
: > "$CALLS"
bash tools/check.sh --quick --run=TestExample ./internal/example > /dev/null
[ "$(cat "$CALLS")" = 'go test -run=TestExample ./internal/example' ]
for args in '--quick' '--quick --full ./internal/example' './internal/example'; do
  if bash tools/check.sh $args > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
done
if FAIL_TESTS=1 bash tools/check.sh --quick ./internal/example > /dev/null 2>&1; then exit 1; else [ "$?" = 7 ]; fi
if FAIL_TESTS=1 bash tools/check.sh > /dev/null 2>&1; then exit 1; else [ "$?" = 1 ]; fi
: > "$CALLS"
bash tools/check.sh --profile > "$scratch/profile.log"
grep -Fxq 'go test -json -count=1 ./...' "$CALLS"
grep -q 'profile logs retained' "$scratch/profile.log"
: > "$CALLS"
if BROKEN_PLAN=1 bash tools/check.sh > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
[ ! -s "$CALLS" ]
rm -f "$CALLS.deps"
bash tools/check.sh --plan > "$scratch/plan.json"
test -f "$CALLS.deps"
if FAIL_DEPS=1 bash tools/check.sh --plan > /dev/null 2>&1; then exit 1; else [ "$?" = 2 ]; fi
[ ! -s "$CALLS" ]
"$REAL_NODE" -e 'const fs=require("fs");const p=JSON.parse(fs.readFileSync(process.argv[1]));if(p.full || p.web)process.exit(1)' "$scratch/plan.json"
echo 'check modes integration: PASS'

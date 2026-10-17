#!/bin/bash
# tools/check.sh [--full] [--web] [--profile]
# tools/check.sh --quick <package>... | --plan
#
# Every check AGENTS.md requires before a commit, in one command, ending with
# one line per check and an exit code: 0 every check passed, 1 at least one
# did not, 2 this script was called wrongly, 75 the compile slot or memory was
# not had within heavy's --max-wait and nothing was checked (run it again),
# or 76 when a queued check was handed to a callback (end this turn and wait
# for its notice). A
# check that answers "could not tell" (check-private.sh's 3, for one) is not a
# pass, so it counts as failed.
#
# Roots and children each used to write their own loop over the list in
# AGENTS.md; this is that loop, kept beside the list so the two change
# together. Every check runs even after one fails, so one run says everything
# that is wrong. The dependency preparation step may populate ignored
# web/node_modules in a linked worktree.
#
# The local gate runs changed-package tests, affected web tests, type checks
# and changed-file privacy checks. Caller compilation is optional (--callers).
# Full runtime suites, cross-platform vet/builds, whole-tree privacy and bundles
# run once for integration/CI, not after every local edit. Unknown/tooling inputs
# fail closed to full checks. --full forces integration; --profile retains timings.
# --quick runs explicitly named Go packages for development, never the gate.
# See docs/checks.md.
#
# The whole script runs once inside tools/heavy.sh, which is what AGENTS.md
# asks for: a `heavy` inside a `heavy` runs directly, so the steps below that
# AGENTS.md writes with tools/heavy.sh share this one slot.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

web=auto
full=no
quick=no
profile=no
callers=no
plan_only=no
packages=()
run_pattern=""
for arg in "$@"; do
  case "$arg" in
    --web) web=yes ;;
    --full) full=yes ;;
    --quick) quick=yes ;;
    --profile) profile=yes ;;
    --callers) callers=yes ;;
    --run=*) run_pattern=${arg#--run=} ;;
    --plan) plan_only=yes ;;
    ./*) packages+=("$arg") ;;
    -h | --help) sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "usage: tools/check.sh [--full] [--web] [--profile] | --quick <package>... | --plan" >&2; exit 2 ;;
  esac
done

if [ "$quick" = yes ]; then
  if [ "${#packages[@]}" -eq 0 ] || [ "$full" != no ] || [ "$web" != auto ] || [ "$profile" != no ] || [ "$plan_only" != no ] || [ "$callers" != no ]; then
    echo '--quick requires explicit ./package paths and cannot be combined with gate options' >&2; exit 2
  fi
elif [ "${#packages[@]}" -ne 0 ] || [ -n "$run_pattern" ]; then
  echo 'package arguments require --quick' >&2; exit 2
fi
if [ "$plan_only" = yes ]; then
  args=(); [ "$full" = no ] && [ "$profile" = no ] || args+=(--full)
  [ "$web" = auto ] || args+=(--web)
  exec node tools/check-scope.mjs ${args[@]+"${args[@]}"}
fi

# heavy.sh without a daemon runs the command directly and sets nothing, so the
# script marks its own second run rather than trusting CLAWDLINE_HEAVY alone.
if [ -z "${CLAWDLINE_HEAVY:-}" ] && [ -z "${CLAWDLINE_CHECK_WRAPPED:-}" ]; then
  export CLAWDLINE_CHECK_WRAPPED=1
  HEAVY_REASON="${HEAVY_REASON:-tools/check.sh}" HEAVY_HANDOFF="${HEAVY_HANDOFF:-1}" exec tools/heavy.sh "$0" "$@"
fi

if [ "$quick" = yes ]; then
  echo 'DEVELOPMENT CHECK ONLY: this does not satisfy the pre-commit gate.'
  run_args=(); [ -z "$run_pattern" ] || run_args+=("-run=$run_pattern")
  exec go test ${run_args[@]+"${run_args[@]}"} ${packages[@]+"${packages[@]}"}
fi

tmp=${TMPDIR:-/tmp}
logs=$(mktemp -d "${tmp%/}/clawdline-check.XXXXXX") || exit 2
scope_args=(); [ "$full" = no ] && [ "$profile" = no ] || scope_args+=(--full)
node tools/check-scope.mjs --out "$logs/scope.json" ${scope_args[@]+"${scope_args[@]}"} || exit 2
scope_full=$(node tools/check-scope.mjs --field "$logs/scope.json" full) || exit 2
case "$scope_full" in true) go_checks=yes; web=yes ;; false) go_checks=no ;; *) echo 'check: invalid scope plan' >&2; exit 2 ;; esac
if [ "$web" = auto ]; then
  scope_web=$(node tools/check-scope.mjs --field "$logs/scope.json" web) || exit 2
  case "$scope_web" in true) web=scoped ;; false) web=no ;; *) exit 2 ;; esac
fi
echo "check plan: $(node tools/check-scope.mjs --field "$logs/scope.json" reason)"
go_tests=() go_compile=() private_paths=() web_tests=()
node tools/check-scope.mjs --field "$logs/scope.json" go.tests --nul > "$logs/go-tests.list" || exit 2
node tools/check-scope.mjs --field "$logs/scope.json" go.compile --nul > "$logs/go-compile.list" || exit 2
node tools/check-scope.mjs --field "$logs/scope.json" paths --nul > "$logs/paths.list" || exit 2
node tools/check-scope.mjs --field "$logs/scope.json" webTests --nul > "$logs/web-tests.list" || exit 2
while IFS= read -r -d '' path; do go_tests+=("$path"); done < "$logs/go-tests.list"
while IFS= read -r -d '' path; do go_compile+=("$path"); done < "$logs/go-compile.list"
while IFS= read -r -d '' path; do private_paths+=("$path"); done < "$logs/paths.list"
while IFS= read -r -d '' path; do web_tests+=("$PWD/$path"); done < "$logs/web-tests.list"
summary=()
failed=0

# check <name> <command…> runs one check, keeps its output, and shows the end
# of it only when the check failed.
check() {
  local name=$1; shift
  local log="$logs/${#summary[@]}.log" began=$SECONDS code
  echo "== $name"
  "$@" >"$log" 2>&1
  code=$?
  if [ "$code" -eq 0 ]; then
    summary+=("PASS  $name  ($((SECONDS - began))s)")
  else
    tail -n 40 "$log" | sed 's/^/   | /'
    summary+=("FAIL  $name  (exit $code, $((SECONDS - began))s, log $log)")
    failed=1
  fi
}

# gofmt answers 0 whatever it lists; a file it lists is the failure.
gofmt_silent() { local out; out=$(gofmt -l internal cmd) || return $?; [ -z "$out" ] || { echo "$out"; return 1; }; }

run_web_tests() { (cd web/console && node --experimental-strip-types --test ${web_tests[@]+"${web_tests[@]}"}); }
check "gofmt -l internal cmd" gofmt_silent
if [ "$go_checks" = yes ]; then
  check "go vet ./..." go vet ./...
  check "go build ./..." go build ./...
  check "linux/amd64 build ./cmd/clawdline" env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
  check "windows/amd64 build ./cmd/clawdline" env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/clawdline
  check "windows go vet ./..." env GOOS=windows go vet ./...
else
  summary+=("SKIP  Go vet and cross-builds  (local scope; integration/CI runs them)")
fi
# Integration runs all packages; local checks select changed packages.
# The explicit uncached guard covers directory walks that Go cannot cache safely.
if [ "$profile" = yes ]; then
  check "go test ./... (profile, uncached)" go test -json -count=1 ./...
  events="$logs/$((${#summary[@]} - 1)).log"
  check "Go test timing report" node tools/check-profile.mjs "$events" "$logs/go-profile.json"
elif [ "$scope_full" = true ]; then
  check "go test ./..." go test ./...
  check "repository copy guards (uncached)" go test -count=1 ./internal/config
else
  if [ "${#go_tests[@]}" -gt 0 ]; then check "changed Go packages" go test ${go_tests[@]+"${go_tests[@]}"}; fi
  if [ "${#go_compile[@]}" -gt 0 ]; then
    if [ "$callers" = yes ]; then check "caller compilation (runtime tests in integration/CI)" go test '-run=^$' ${go_compile[@]+"${go_compile[@]}"}; else summary+=("SKIP  caller compilation and runtime tests  (${#go_compile[@]} packages listed by --plan; integration/CI or --callers)"); fi
  fi
  check "repository copy guards (uncached)" go test -count=1 ./internal/config
  check "capacity registration and copy guards" go test -count=1 ./internal/domain/capacity '-run=^(TestEveryBoundIsRegistered|TestRegisterRowsAnswerTheFourQuestions|TestEveryRowExplainsItselfToAPerson)$'
fi
check "contract-gen -check" go run ./tools/contract-gen -check
check "check-legacy-css.sh" tools/check-legacy-css.sh
check "web dependencies" node tools/ensure-web-deps.mjs
check "check-rounded-side-borders.mjs" node web/console/tools/check-rounded-side-borders.mjs
check "check-machine-words.sh" tools/check-machine-words.sh
check "check-shell-expansions.sh" tools/check-shell-expansions.sh
if [ "$scope_full" = true ]; then
  check "check-private.sh (whole tree)" tools/check-private.sh
elif [ "${#private_paths[@]}" -gt 0 ]; then
  check "check-private.sh (changed files; whole tree in integration)" tools/check-private.sh -- ${private_paths[@]+"${private_paths[@]}"}
fi
check "check-console-tests.mjs" node tools/check-console-tests.mjs
check "check-mirror-tests.mjs" node tools/check-mirror-tests.mjs
check "test-notify-release-success.sh" sh tools/test-notify-release-success.sh
if [ "$(uname -s)" = Linux ]; then
  check "test-heavy-linux-cache.sh" bash tools/test-heavy-linux-cache.sh
  check "test-linux-dev-build.sh" bash tools/test-linux-dev-build.sh
fi
check "check planning and profiling tests" node --test tools/check-plan.test.mjs tools/check-profile.test.mjs tools/check-scope.test.mjs
check "check modes integration" bash tools/test-check-modes.sh
if [ "$(uname -s)" = Darwin ]; then
  check "Swift staged app watcher" sh tools/test-staged-app-watcher.sh
fi
if [ "$web" = yes ]; then
  check "web: npm run check" sh -c 'cd web && npm run deps && npm run check'
  check "web: npm run build" sh -c 'cd web && npm run build'
  check "web: npm test" sh -c 'cd web && npm test'
elif [ "$web" = scoped ]; then
  check "web: incremental type-check" sh -c 'cd web && node node_modules/typescript/bin/tsc --noEmit -p console --incremental --tsBuildInfoFile .clawdline-check.tsbuildinfo'
  check "web: source catalog" node web/console/tools/check-source-catalog.mjs
  check "web: catalogs" node web/console/tools/check-catalogs.mjs
  check "web: semantic risk keys" node web/console/tools/semantic-risk-keys.mjs --check
  check "web: semantic screen groups" node web/console/tools/group-semantic-keys.mjs --check
  check "web: refusal copy" node web/console/tools/check-refusal-text.mjs
  if [ "${#web_tests[@]}" -gt 0 ]; then check "web: affected tests" run_web_tests; else summary+=("SKIP  web runtime tests  (no affected test imports; integration/CI runs the full suite)"); fi
  summary+=("SKIP  web bundle  (integration, CI and release)")
else
  summary+=("SKIP  web checks  (nothing under web/ changed; --web runs them)")
fi

echo
printf '%s\n' ${summary[@]+"${summary[@]}"}
if [ "$failed" -eq 0 ]; then
  if [ "$profile" = yes ]; then echo "check: profile logs retained at $logs"; else rm -rf "$logs"; fi
  echo "check: every check passed"
else
  echo "check: FAILED; each failed check's whole output is in $logs"
fi
exit "$failed"

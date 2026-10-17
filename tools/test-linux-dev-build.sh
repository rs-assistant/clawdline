#!/bin/bash
# Exercise the build boundary with fake compilers, never the running service.
set -euo pipefail
script=$(cd "$(dirname "$0")" && pwd)/build-linux-user-release.sh
scratch=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-dev-build-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/repo/web" "$scratch/bin"
cd "$scratch/repo"
git init -q
git -c user.name=Test -c user.email=test@example.invalid commit --allow-empty -qm fixture
commit=$(git rev-parse HEAD)
export DEV_BUILD_TEST_LOG=$scratch/commands
cat > "$scratch/bin/go" <<'GO'
#!/bin/bash
printf 'go %s\n' "$*" >> "$DEV_BUILD_TEST_LOG"
if [ "$1" = test ] && [ "${FAIL_TESTS:-}" = 1 ]; then exit 9; fi
if [ "$1" = build ]; then
 while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; printf '#!/bin/sh\nexit 0\n' > "$1"; chmod +x "$1"; break; fi
  shift
 done
fi
GO
cat > "$scratch/bin/npm" <<'NPM'
#!/bin/bash
printf 'npm %s\n' "$*" >> "$DEV_BUILD_TEST_LOG"
if [ "$*" = 'run check' ] && [ "${FAIL_TESTS:-}" = 1 ]; then exit 9; fi
if [ "$*" = 'run build' ]; then mkdir -p console/dist/assets; printf '// local\n' > console/dist/assets/main-test.js; fi
NPM
chmod +x "$scratch/bin/go" "$scratch/bin/npm"
export PATH=$scratch/bin:$PATH
FAIL_TESTS=1 bash "$script" "$scratch/dev" "$commit" --dev > "$scratch/dev.log"
test -x "$scratch/dev/clawdline"
! rg -q '^go (test|vet)|^npm run check' "$DEV_BUILD_TEST_LOG"
rg -q -- "-X main.releaseVersion=v0.0.0-dev.${commit:0:7}" "$DEV_BUILD_TEST_LOG"
rg -q "$commit" "$scratch/dev/dist/BUILD.json"
: > "$DEV_BUILD_TEST_LOG"
if FAIL_TESTS=1 bash "$script" "$scratch/failed" "$commit" > "$scratch/failed.log" 2>&1; then echo 'full build ignored test failure' >&2; exit 1; fi
test ! -e "$scratch/failed"
rg -q '^go test' "$DEV_BUILD_TEST_LOG"
: > "$DEV_BUILD_TEST_LOG"
bash "$script" "$scratch/full" "$commit" > "$scratch/full.log"
rg -q '^go test' "$DEV_BUILD_TEST_LOG"
rg -q '^go vet' "$DEV_BUILD_TEST_LOG"
rg -q '^npm run check' "$DEV_BUILD_TEST_LOG"
if bash "$script" "$scratch/invalid" "$commit" --unknown > /dev/null 2>&1; then exit 1; fi
echo 'Linux dev build boundary: PASS'

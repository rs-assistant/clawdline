#!/bin/bash
# The publication gate for a drafted release (docs/releasing.md). Run on a
# machine that has the private word list (.git/info/private-words); CI has
# none, so CI can draft a release but never decide it may be published.
#
# usage: tools/release/gate.sh <version> [--publish] [--dir <local release dir>]
#          [--bundle-dir D --trusted-root F]
#
# 1. downloads the draft's assets (or reads --dir, for a local test release),
# 2. verifies manifest.sig.json with the keys compiled into this checkout and
#    every artifact's size and sha256, and refuses any asset it does not know,
# 3. checks each archive's SBOM against the archive (tools/release/sbom.sh),
# 4. checks every asset's build provenance (tools/release/provenance.sh;
#    offline with --bundle-dir and --trusted-root),
# 5. checks the macOS signatures, hardened runtime and notarization
#    (codesign_verify_release in codesign.sh); this needs a Mac,
# 6. unpacks every archive and runs tools/check-private.sh -dir over all of it,
#    binaries and SBOMs included,
# 7. with --publish, and only when all of that passed, un-drafts the release.
#
# A -test.N release read with --dir was not built by the workflow and is not
# signed for distribution, so steps 4 and 5 are skipped for it and say so. A
# -test.N version is gated only that way: one drafted on GitHub is refused
# rather than downloaded without those steps. Every other version fails closed: missing evidence is a failed gate, not a warning.
# A release drafted before this gate existed has no SBOM or attestation and
# cannot pass it; that is the intended answer, not a reason to loosen it.
set -euo pipefail
cd "$(dirname "$0")/../.."

[ "$#" -ge 1 ] || { echo "usage: tools/release/gate.sh <version> [--publish] [--dir D]" >&2; exit 2; }
version=$1; shift
publish=0 dir="" offline=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --publish) publish=1 ;;
    --dir) dir=${2:?}; shift ;;
    --bundle-dir|--trusted-root) offline+=("$1" "${2:?}"); shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
  shift
done
[ "$publish" = 0 ] || [ -z "$dir" ] || { echo "--publish is for a drafted GitHub release, not --dir" >&2; exit 2; }
test_release=0
if [[ "$version" =~ -test\.[0-9]+$ ]]; then
  [ -n "$dir" ] || { echo "a -test release is gated only with --dir; it has no provenance or notarization to check" >&2; exit 2; }
  test_release=1
fi

work=$(mktemp -d)
# finished: macOS's /bin/bash 3.2 exits 0 after an unbound-variable error when
# an EXIT trap runs, so a build that stopped half-way reported success
# (v0.10.0, 2026-10-07). A run that did not reach its last line fails.
finished=0
trap 's=$?; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT
if [ -z "$dir" ]; then
  draft=$(gh release view "$version" -R sainteye/clawdline --json isDraft -q .isDraft)
  [ "$draft" = true ] || { echo "$version is not a draft; nothing to gate" >&2; exit 1; }
  gh release download "$version" -R sainteye/clawdline --dir "$work/assets"
  dir="$work/assets"
fi

extra=()
[ "$test_release" = 0 ] || extra=(-pub "${CLAWDLINE_TEST_RELEASE_PUB:?a -test release is verified with CLAWDLINE_TEST_RELEASE_PUB}")
go run ./tools/release verify ${extra[@]+"${extra[@]}"} "$dir"

# The manifest must be this version's, and the release must hold nothing but
# its archives, their SBOMs and the signed manifest: an asset nobody checks is
# an asset nobody should publish.
node - "$dir" "$version" <<'JS'
const fs = require("fs"), path = require("path");
const [dir, version] = process.argv.slice(2);
const m = JSON.parse(fs.readFileSync(path.join(dir, "manifest.json")));
if (m.version !== version) { console.error(`gate: manifest.json is ${m.version}, not ${version}`); process.exit(1); }
const known = new Set(["manifest.json", "manifest.sig.json", "SHA256SUMS"]);
for (const a of m.artifacts) { known.add(a.name); known.add(a.name + ".spdx.json"); }
const extra = fs.readdirSync(dir).filter((f) => !known.has(f));
if (extra.length) { console.error(`gate: assets the manifest does not account for: ${extra.join(", ")}`); process.exit(1); }
JS
tools/release/sbom.sh verify "$dir"
if [ "$test_release" = 1 ]; then
  echo "provenance: skipped for a -test release, which the workflow never builds"
else
  tools/release/provenance.sh "$dir" ${offline[@]+"${offline[@]}"}
fi

mkdir -p "$work/unpacked"
for archive in "$dir"/*.tar.gz; do
  d="$work/unpacked/$(basename "$archive" .tar.gz)"
  mkdir -p "$d"
  tar -xzf "$archive" -C "$d"
done
cp "$dir/manifest.json" "$dir/manifest.sig.json" "$dir/SHA256SUMS" "$dir"/*.spdx.json "$work/unpacked/"

if [ "$test_release" = 1 ]; then
  echo "macOS signature and notarization: skipped for a -test release"
else
  [ "$(uname -s)" = Darwin ] || { echo "the macOS signature and notarization check needs a Mac; not publishing" >&2; exit 1; }
  # shellcheck source=tools/release/codesign.sh
  . tools/release/codesign.sh
  mac=()
  for d in "$work"/unpacked/clawdline_*_darwin_*; do
    [ -d "$d" ] && mac+=("$d/clawdline" "$d/libexec/tmux")
  done
  for d in "$work"/unpacked/Clawdline-*-macos-*; do
    [ -d "$d" ] && while IFS= read -r app; do mac+=("$app"); done < <(find "$d" -maxdepth 1 -name '*.app')
  done
  [ "${#mac[@]}" -gt 0 ] || { echo "the release holds no macOS binary to check" >&2; exit 1; }
  codesign_verify_release ${mac[@]+"${mac[@]}"}
fi

set +e
tools/check-private.sh -dir "$work/unpacked"
rc=$?
set -e
case "$rc" in
  0) echo "privacy gate: clean" ;;
  3) echo "privacy gate: UNDETERMINED (no private word list); not publishing" >&2; exit 3 ;;
  *) echo "privacy gate: FAILED ($rc); the draft stays a draft" >&2; exit 1 ;;
esac

if [ "$publish" = 1 ]; then
  latest=()
  [[ "$version" == *-* ]] || latest=(--latest)
  gh release edit "$version" -R sainteye/clawdline --draft=false ${latest[@]+"${latest[@]}"}
  echo "published $version"
else
  echo "gate passed; publish with: tools/release/gate.sh $version --publish"
fi
finished=1

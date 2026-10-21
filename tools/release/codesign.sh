# Sourced by tools/release/build.sh and tools/package-macos.sh: how a macOS
# binary or bundle of Clawdline is signed.
#
# A stable certificate alone does not preserve all grants. Securityd rejected
# the self-signed certificate for WebKit's keychain item and remembered each
# build's cdhash. Official releases require a Developer ID Application identity;
# development builds can still use an ad hoc or local identity.
#
# CLAWDLINE_CODESIGN_P12 names the certificate and key as a .p12 file, and
# CLAWDLINE_CODESIGN_PASSWORD its password. Without them every signature is ad
# hoc, as before: a clone builds and runs without anyone's certificate.

codesign_begin() {
  [ -z "${CLAWDLINE_CODESIGN_ID:-}" ] || return 0 # an outer build already began
  if [ -z "${CLAWDLINE_CODESIGN_P12:-}" ]; then
    export CLAWDLINE_CODESIGN_ID=-
    return 0
  fi
  [ -f "$CLAWDLINE_CODESIGN_P12" ] || { echo "CLAWDLINE_CODESIGN_P12 is not a file: $CLAWDLINE_CODESIGN_P12" >&2; return 1; }
  codesign_owner=1
  codesign_dir=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-sign.XXXXXX")
  codesign_keychain="$codesign_dir/sign.keychain-db"
  codesign_kcpass=$(openssl rand -hex 16)
  codesign_search=$(security list-keychains -d user | sed 's/^ *"//; s/"$//')
  security create-keychain -p "$codesign_kcpass" "$codesign_keychain" >/dev/null
  security set-keychain-settings "$codesign_keychain" # no timeout lock
  security unlock-keychain -p "$codesign_kcpass" "$codesign_keychain"
  security import "$CLAWDLINE_CODESIGN_P12" -k "$codesign_keychain" -P "${CLAWDLINE_CODESIGN_PASSWORD:-}" \
    -T /usr/bin/codesign >/dev/null
  # Let codesign use the key without a dialog.
  security set-key-partition-list -S apple-tool:,apple: -s -k "$codesign_kcpass" "$codesign_keychain" >/dev/null
  # codesign finds an identity only on the search list; it goes back as it was
  # in codesign_end.
  local list=()
  while IFS= read -r k; do [ -n "$k" ] && list+=("$k"); done <<<"$codesign_search"
  security list-keychains -d user -s ${list[@]+"${list[@]}"} "$codesign_keychain"
  CLAWDLINE_CODESIGN_ID=$(security find-identity -p codesigning "$codesign_keychain" |
    awk '/[0-9A-F]{40}/ { print $2; exit }')
  [ -n "$CLAWDLINE_CODESIGN_ID" ] || { codesign_end; echo "no code-signing identity in $CLAWDLINE_CODESIGN_P12" >&2; return 1; }
  export CLAWDLINE_CODESIGN_ID
  echo "signing with certificate $CLAWDLINE_CODESIGN_ID"
}

# codesign_path signs each path with the build's identity. An official
# release (CLAWDLINE_CODESIGN_HARDENED=1, set by build.sh) also asks for the
# hardened runtime and a secure timestamp, which notarization requires. No
# entitlements are granted: nothing Clawdline ships loads unsigned code, uses
# a JIT or sends Apple Events.
codesign_path() {
  local p opts=()
  [ "${CLAWDLINE_CODESIGN_HARDENED:-0}" != 1 ] || opts=(--options runtime --timestamp)
  for p in "$@"; do
    codesign --force ${opts[@]+"${opts[@]}"} --sign "${CLAWDLINE_CODESIGN_ID:--}" "$p"
  done
}

# codesign_notarize <zip-name> <path>... submits the paths to Apple's notary
# service as one zip and waits for its verdict; an .app among them is then
# stapled. It needs an App Store Connect API key: CLAWDLINE_NOTARY_KEY (the
# .p8 file), CLAWDLINE_NOTARY_KEY_ID and CLAWDLINE_NOTARY_ISSUER. Without them
# it fails: an official release is never published un-notarized. The
# submission's id and status are appended to $CLAWDLINE_NOTARY_LOG when set.
codesign_notarize() {
  local name=$1 p zipdir result status sid
  shift
  [ -n "${CLAWDLINE_NOTARY_KEY:-}" ] && [ -f "$CLAWDLINE_NOTARY_KEY" ] &&
    [ -n "${CLAWDLINE_NOTARY_KEY_ID:-}" ] && [ -n "${CLAWDLINE_NOTARY_ISSUER:-}" ] || {
      echo "notarization needs CLAWDLINE_NOTARY_KEY, CLAWDLINE_NOTARY_KEY_ID and CLAWDLINE_NOTARY_ISSUER" >&2
      return 1
    }
  zipdir=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-notary.XXXXXX")
  mkdir -p "$zipdir/$name"
  for p in "$@"; do ditto "$p" "$zipdir/$name/$(basename "$p")"; done
  ditto -c -k --keepParent "$zipdir/$name" "$zipdir/$name.zip"
  result=$(xcrun notarytool submit "$zipdir/$name.zip" --key "$CLAWDLINE_NOTARY_KEY" \
    --key-id "$CLAWDLINE_NOTARY_KEY_ID" --issuer "$CLAWDLINE_NOTARY_ISSUER" --wait --output-format json) || {
      rm -rf "$zipdir"; echo "notarytool submit failed for $name" >&2; return 1; }
  rm -rf "$zipdir"
  status=$(printf '%s' "$result" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>process.stdout.write(JSON.parse(s).status||""))')
  sid=$(printf '%s' "$result" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>process.stdout.write(JSON.parse(s).id||""))')
  [ -z "${CLAWDLINE_NOTARY_LOG:-}" ] || printf '{"name":"%s","submission":"%s","status":"%s"}\n' "$name" "$sid" "$status" >>"$CLAWDLINE_NOTARY_LOG"
  if [ "$status" != Accepted ]; then
    echo "notarization of $name was $status (submission $sid):" >&2
    xcrun notarytool log "$sid" --key "$CLAWDLINE_NOTARY_KEY" --key-id "$CLAWDLINE_NOTARY_KEY_ID" \
      --issuer "$CLAWDLINE_NOTARY_ISSUER" >&2 || true
    return 1
  fi
  echo "notarized $name (submission $sid)"
  for p in "$@"; do
    case "$p" in *.app) xcrun stapler staple "$p" && xcrun stapler validate "$p" ;; esac
  done
}

# codesign_verify_release <path>... is the publication check of a shipped
# macOS binary or app: a strict, deep signature by a Developer ID Application
# identity with the hardened runtime and a secure timestamp. An .app must also
# carry a stapled notarization ticket and pass Gatekeeper. A bare binary
# cannot be stapled; its notarization is the build's notarytool verdict, which
# Gatekeeper looks up online when the binary is first run.
codesign_verify_release() {
  local p info
  for p in "$@"; do
    codesign --verify --deep --strict "$p" || { echo "$p: signature does not verify" >&2; return 1; }
    info=$(codesign -dvvv "$p" 2>&1)
    grep -q '^Authority=Developer ID Application: ' <<<"$info" || { echo "$p: not signed by a Developer ID Application identity" >&2; return 1; }
    grep -Eq '^CodeDirectory .*flags=0x[0-9a-f]+\([^)]*runtime' <<<"$info" || { echo "$p: no hardened runtime" >&2; return 1; }
    grep -q '^Timestamp=' <<<"$info" || { echo "$p: no secure timestamp" >&2; return 1; }
    case "$p" in
      *.app)
        xcrun stapler validate "$p" >/dev/null || { echo "$p: no stapled notarization ticket" >&2; return 1; }
        spctl --assess --type execute "$p" || { echo "$p: Gatekeeper refuses it" >&2; return 1; }
        echo "$p: notarization ticket stapled; Gatekeeper accepts it"
        ;;
    esac
    echo "$p: Developer ID, hardened runtime, timestamped"
  done
}

codesign_require_developer_id() {
  [ "${CLAWDLINE_CODESIGN_ID:-}" != - ] || { echo "official macOS releases require a Developer ID Application identity" >&2; return 1; }
  security find-identity -v -p codesigning |
    grep -F "${CLAWDLINE_CODESIGN_ID:-missing}" |
    grep -q 'Developer ID Application:' || {
      echo "official macOS releases require a Developer ID Application identity" >&2
      return 1
    }
}

codesign_end() {
  [ "${codesign_owner:-0}" = 1 ] || return 0
  local list=()
  while IFS= read -r k; do [ -n "$k" ] && list+=("$k"); done <<<"$codesign_search"
  [ ${#list[@]} = 0 ] || security list-keychains -d user -s ${list[@]+"${list[@]}"}
  security delete-keychain "$codesign_keychain" 2>/dev/null || true
  rm -rf "$codesign_dir"
  codesign_owner=0
  unset CLAWDLINE_CODESIGN_ID
}

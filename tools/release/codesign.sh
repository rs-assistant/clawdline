# Sourced by tools/release/build.sh and tools/package-macos.sh: how a macOS
# binary or bundle of Clawdline is signed.
#
# With an ad hoc signature (`codesign --sign -`) a build's identity is its
# cdhash, so every build is, to macOS, another program: a reinstall or an update
# asked again for Dropbox, for the local network, and for the keychain password
# behind "Clawdline Next WebCrypto Master Key" (2026-10-16). Signed with one
# certificate, the designated requirement is `identifier … and certificate leaf
# = H"…"`, the same for every build, and what the person allowed once stays
# allowed. The certificate is self-signed; codesign accepts it untrusted, and
# nothing here asks Gatekeeper, which an installer that does not quarantine
# never consults.
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
  security list-keychains -d user -s "${list[@]}" "$codesign_keychain"
  CLAWDLINE_CODESIGN_ID=$(security find-identity -p codesigning "$codesign_keychain" |
    awk '/[0-9A-F]{40}/ { print $2; exit }')
  [ -n "$CLAWDLINE_CODESIGN_ID" ] || { codesign_end; echo "no code-signing identity in $CLAWDLINE_CODESIGN_P12" >&2; return 1; }
  export CLAWDLINE_CODESIGN_ID
  echo "signing with certificate $CLAWDLINE_CODESIGN_ID"
}

# codesign_path signs each path with the build's identity.
codesign_path() {
  local p
  for p in "$@"; do
    codesign --force --sign "${CLAWDLINE_CODESIGN_ID:--}" "$p"
  done
}

codesign_end() {
  [ "${codesign_owner:-0}" = 1 ] || return 0
  local list=()
  while IFS= read -r k; do [ -n "$k" ] && list+=("$k"); done <<<"$codesign_search"
  [ ${#list[@]} = 0 ] || security list-keychains -d user -s "${list[@]}"
  security delete-keychain "$codesign_keychain" 2>/dev/null || true
  rm -rf "$codesign_dir"
  codesign_owner=0
  unset CLAWDLINE_CODESIGN_ID
}

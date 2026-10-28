#!/bin/sh
# Install Clawdline on macOS or Linux, as this user, with no sudo:
#
#   curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --headless --port 7800
#
# This script downloads a release's manifest, checks its Ed25519 signature
# with the release keys written below — using openssl or ssh-keygen, never
# anything it downloaded — downloads the archive the signed manifest names,
# checks its sha256, and only then unpacks it into
# ~/.local/share/clawdline-next/releases/<version>/. Everything else — tmux,
# the service, the app, the health check — is `clawdline setup` from inside
# that release, which this ends by running with every option it was given
# (`clawdline setup --help` lists them). Running it again repairs or upgrades;
# `--uninstall` removes it, and `--uninstall --purge` the state directory too,
# even after the binary is gone.
#
#   --version vX.Y.Z             that release instead of the latest, whatever its age
#   --channel beta               the newest release including pre-releases
#   --allow-downgrade            install a release older than the one installed
#   --verbose                    where each file came from and went
#   CLAWDLINE_INSTALL_OPENSSL    an OpenSSL 3 binary to check the signature with
#   CLAWDLINE_INSTALL_BASE_URL   where the manifest and the archives are (tests)
#   CLAWDLINE_INSTALL_TEST_KEY   one more trusted key, only with the line above (tests)
set -eu
# Nothing this script writes — the manifest, the archive, the unpacked release —
# is readable by another user of the machine while it is being checked.
umask 077

repo_url=https://github.com/sainteye/clawdline

# The public halves of the keys that sign releases, base64 Ed25519, separated
# by spaces: the list in internal/adapters/release/keys.go, which
# TestInstallScriptCarriesTheReleaseKeysAndBounds holds this to.
release_keys='2Du3yZ4jZF8zoMcZ2ANHFNbxZuz0wgAZdR0nZJ/g/O0='
# Without --version, the newest release is taken only when its signed
# committed_at is at most this many days old: a signed manifest never expires
# by itself, so an old one could otherwise be served as the newest for ever
# (internal/adapters/release.maxManifestAgeSeconds, in days).
max_manifest_age_days=180
# The namespace of the SSH copy of the signature (release.SSHNamespace).
ssh_namespace=clawdline-release

say() { printf '%s\n' "$*"; }
detail() { [ "$verbose" = 0 ] || say "  $*"; }
# die says what stopped the install, and every message ends with the next step.
die() { printf 'clawdline install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# fetch <url> <file> <max bytes>: 0 when the file arrived; 22 for a 404, so
# "no release yet" can be told from a network failure. A body larger than its
# bound is refused, whatever the server says its length is.
#
# Only curl: it keeps every request and redirect on https. wget cannot —
# GNU wget's --https-only applies only to recursive downloads (measured with
# Wget 1.21.4: it fetched a plain http URL and followed an https→http
# redirect), and BusyBox wget has no such option at all.
fetch() {
  have curl || die "curl is not installed. Install curl (wget cannot keep a download on https) and run this again."
  # https only, redirects included; plain http only for a test server named
  # by CLAWDLINE_INSTALL_BASE_URL.
  proto='=https'
  case ${CLAWDLINE_INSTALL_BASE_URL:-} in http://*) proto='=https,http' ;; esac
  code=$(curl -sSL --proto "$proto" --proto-redir "$proto" --retry 2 --max-filesize "$3" -o "$2" -w '%{http_code}' "$1") || return 1
  [ ! -f "$2" ] || [ "$(wc -c <"$2" | tr -d ' ')" -le "$3" ] || return 1
  [ "$code" = 200 ] && return 0
  [ "$code" = 404 ] && return 22
  return 1
}

sha256_of() {
  if have shasum; then shasum -a 256 "$1" | cut -d' ' -f1
  elif have sha256sum; then sha256sum "$1" | cut -d' ' -f1
  else die "neither shasum nor sha256sum is installed. Install coreutils (sha256sum) and run this again."
  fi
}

platform() {
  case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "Clawdline installs on macOS and Linux; this is $(uname -s). Run this on a Mac or a Linux machine." ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "Clawdline runs on amd64 and arm64; this machine is $(uname -m). Run this on an amd64 or arm64 machine." ;;
  esac
  # A shell under Rosetta says x86_64 on an Apple silicon Mac.
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
    arch=arm64
  fi
}

# newest_tag: the newest release on the beta channel, pre-releases included,
# from the Releases API (newest first). The list is not signed; the manifest
# of the tag it names is, and is checked like any other.
newest_tag() {
  fetch "https://api.github.com/repos/sainteye/clawdline/releases?per_page=10" "$work/releases.json" 1048576 ||
    die "could not list the releases at $repo_url/releases. Check your network, or pass --version vX.Y.Z."
  sed -n 's/^ *"tag_name": *"\(v[0-9][^"]*\)".*/\1/p' "$work/releases.json" | head -n 1
}

# b64d / b64e: base64 on stdin to bytes on stdout, and back.
b64d() { if have "$openssl"; then "$openssl" base64 -d -A; else base64 -d; fi; }
b64e() { if have "$openssl"; then "$openssl" base64 -A; else base64 | tr -d '\n'; fi; }

# key_der <base64 key> <file>: the key as a DER SubjectPublicKeyInfo, which
# is a fixed 12-byte Ed25519 prefix and the 32 key bytes.
key_der() {
  { printf '\060\052\060\005\006\003\053\145\160\003\041\000'; printf '%s' "$1" | b64d; } >"$2"
}

# ossl_verify <base64 key> <message file> <signature file>
ossl_verify() {
  key_der "$1" "$work/key.der"
  "$openssl" pkeyutl -verify -pubin -keyform DER -inkey "$work/key.der" -rawin -in "$2" -sigfile "$3" >/dev/null 2>&1
}

# openssl_can: whether $openssl verifies Ed25519 (OpenSSL 3.0 and later; the
# LibreSSL macOS ships, and OpenSSL 1.1, do not), proved on RFC 8032's test
# 2 rather than guessed from a version string.
openssl_can() {
  have "$openssl" || return 1
  printf r >"$work/probe.msg"
  printf '%s' 'kqAJqfDUyrhyDoILX2QlQKKye1QWUD+Ps3YiI+vbadoIWsHkPhWZbkWPNhPQ8R2MOHsurrQwKu6wDSkWErsMAA==' | b64d >"$work/probe.sig" 2>/dev/null || return 1
  ossl_verify 'PUAXw+hDiVqStwqnTRt+vJyYLM8uxJaMwM1V8Sr0Zgw=' "$work/probe.msg" "$work/probe.sig"
}

# ssh_verify: 0 when manifest.sshsig is a good signature over manifest.json
# by one of $keys, 1 when it is not, 2 when it cannot be asked (no
# ssh-keygen, one older than OpenSSH 8.1, or a release with no manifest.sshsig).
ssh_verify() {
  have ssh-keygen && [ -s "$work/manifest.sshsig" ] || return 2
  : >"$work/allowed_signers"
  for k in $keys; do
    w=$({ printf '\000\000\000\013ssh-ed25519\000\000\000\040'; printf '%s' "$k" | b64d; } | b64e)
    printf '%s ssh-ed25519 %s\n' "$ssh_namespace" "$w" >>"$work/allowed_signers"
  done
  if ssh-keygen -Y verify -f "$work/allowed_signers" -I "$ssh_namespace" -n "$ssh_namespace" \
    -s "$work/manifest.sshsig" <"$work/manifest.json" >"$work/ssh.out" 2>&1; then
    return 0
  fi
  grep -q 'option -- Y' "$work/ssh.out" && return 2
  return 1
}

# verify_release: manifest.json is signed by a release key, or the install
# stops here, before anything downloaded has run.
verify_release() {
  keys=$release_keys
  # A test release is signed with a key of its own (main refuses the key
  # without CLAWDLINE_INSTALL_BASE_URL).
  [ -z "${CLAWDLINE_INSTALL_TEST_KEY:-}" ] || keys="$keys $CLAWDLINE_INSTALL_TEST_KEY"
  [ "$(wc -c <"$work/manifest.json" | tr -d ' ')" -le 65536 ] ||
    die "the release's manifest.json is longer than a manifest may be; nothing was installed. Report it at $repo_url/issues."
  if openssl_can; then
    for k in $keys; do
      printf '%s' "$k" | b64d >"$work/key.raw" 2>/dev/null || true
      [ "$(wc -c <"$work/key.raw" | tr -d ' ')" = 32 ] || die "release key $k is not a base64 Ed25519 key; nothing was installed."
    done
    # manifest.sig.json is not signed itself; each signature in it either
    # verifies or does not, so its shape is only read loosely.
    for s in $(tr '{},' '\n\n\n' <"$work/manifest.sig.json" | sed -n 's/^[[:space:]]*"sig":[[:space:]]*"\([A-Za-z0-9+/=]*\)"[[:space:]]*$/\1/p'); do
      printf '%s' "$s" | b64d >"$work/sig.bin" 2>/dev/null || continue
      [ "$(wc -c <"$work/sig.bin" | tr -d ' ')" = 64 ] || continue
      for k in $keys; do
        if ossl_verify "$k" "$work/manifest.json" "$work/sig.bin"; then
          detail "signature checked with $openssl"
          return 0
        fi
      done
    done
    die "the release's manifest is not signed by a Clawdline release key (checked with $openssl); nothing was run or installed. The download may have been tampered with: run this again later, and if it repeats, report it at $repo_url/issues."
  fi
  status=0
  ssh_verify || status=$?
  case $status in
    0) detail "signature checked with ssh-keygen"; return 0 ;;
    1) die "the release's manifest.sshsig is not a signature by a Clawdline release key over its manifest (ssh-keygen: $(tr '\n' ' ' <"$work/ssh.out")); nothing was run or installed. The download may have been tampered with: run this again later, and if it repeats, report it at $repo_url/issues." ;;
  esac
  case $os in
    darwin) next='Install OpenSSL 3 (brew install openssl@3) and run this again with CLAWDLINE_INSTALL_OPENSSL="$(brew --prefix openssl@3)/bin/openssl"' ;;
    *) next='Install OpenSSL 3 (the openssl package) or OpenSSH 8.1 or later (ssh-keygen), and run this again' ;;
  esac
  die "nothing here can check this release's Ed25519 signature: $openssl is missing or older than OpenSSL 3, and ssh-keygen is missing, older than OpenSSH 8.1, or the release carries no manifest.sshsig. Nothing was installed; the downloaded files are not run unchecked. $next."
}

# field <name>: a top-level string field of the verified manifest. A field
# named twice is refused: a JSON reader elsewhere could take the other one,
# and the two would no longer be describing the same release.
field() {
  n=$(grep -c "^[[:space:]]*\"$1\":" "$work/manifest.lines" || true)
  [ "$n" -le 1 ] || die "the release's manifest names \"$1\" more than once; nothing was installed. Report it at $repo_url/issues."
  sed -n "s/^[[:space:]]*\"$1\":[[:space:]]*\"\([^\"]*\)\"[[:space:]]*\$/\1/p" "$work/manifest.lines" | head -n 1
}

# days_of YYYY-MM-DD: days since 1970-01-01.
days_of() {
  printf '%s\n' "$1" | awk -F- '{ y = $1 + 0; m = $2 + 0; d = $3 + 0; if (m <= 2) { y--; m += 12 }
    print 365 * y + int(y / 4) - int(y / 100) + int(y / 400) + int((153 * (m - 3) + 2) / 5) + d - 719469 }'
}

# older <a> <b>: 0 when version a sorts before version b, as
# release.Compare does (a pre-release before its release; dotted pre-release
# fields numerically where both are numbers).
older() {
  LC_ALL=C awk -v a="$1" -v b="$2" '
    function num(s) { return s ~ /^[0-9]+$/ }
    function cmp(x, y,   xc, yc, xp, yp, i, n, m, xs, ys, xf, yf) {
      sub(/^v/, "", x); sub(/^v/, "", y)
      xp = ""; yp = ""
      if ((i = index(x, "-")) > 0) { xp = substr(x, i + 1); x = substr(x, 1, i - 1) }
      if ((i = index(y, "-")) > 0) { yp = substr(y, i + 1); y = substr(y, 1, i - 1) }
      split(x, xc, "."); split(y, yc, ".")
      for (i = 1; i <= 3; i++) if (xc[i] + 0 != yc[i] + 0) return xc[i] + 0 < yc[i] + 0 ? -1 : 1
      if (xp == yp) return 0
      if (xp == "") return 1
      if (yp == "") return -1
      n = split(xp, xf, "."); m = split(yp, yf, ".")
      for (i = 1; i <= n && i <= m; i++) {
        if (xf[i] == yf[i]) continue
        if (num(xf[i]) && num(yf[i])) return xf[i] + 0 < yf[i] + 0 ? -1 : 1
        return xf[i] < yf[i] ? -1 : 1
      }
      return n < m ? -1 : (n > m ? 1 : 0)
    }
    BEGIN { exit cmp(a, b) < 0 ? 0 : 1 }'
}

# purge_state removes the state directory (devices, sessions, settings) the
# way `setup --uninstall --purge` does: while a terminal the daemon started
# still runs, the directory its tmux server listens in stays, and is said.
purge_state() {
  state=${CLAWDLINE_NEXT_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/clawdline-next}
  if [ ! -d "$state" ]; then
    say "nothing to purge: $state does not exist"
    return
  fi
  sock=$state/tmux/term.sock
  # Without a tmux of its own the machine ran the one the release carried,
  # which is gone with the release: nothing here can ask that server, so a
  # socket that is there is kept rather than taken from under its terminals.
  if [ -S "$sock" ] && { ! have tmux || [ -n "$(tmux -S "$sock" ls 2>/dev/null)" ]; }; then
    find "$state" -mindepth 1 -maxdepth 1 ! -path "$state/tmux" -exec rm -rf {} +
    say "removed $state (--purge), all but $state/tmux"
    say "kept $state/tmux: the terminals the daemon started may still be open; they end when you exit them, and then it can go"
    return
  fi
  rm -rf "$state"
  say "removed $state (--purge): devices, sessions and settings"
}

main() {
  version="" channel="" uninstall=0 purge=0 verbose=0 downgrade=0
  n=$#
  while [ "$n" -gt 0 ]; do
    a=$1
    shift
    n=$((n - 1))
    case $a in
      --version)
        [ "$n" -gt 0 ] || die "--version needs a version, like v0.10.0"
        version=$1
        shift
        n=$((n - 1))
        continue
        ;;
      --version=*) version=${a#*=}; continue ;;
      # Decided here, before the release runs; a release's setup that
      # predates the option would refuse it.
      --allow-downgrade) downgrade=1; continue ;;
      --channel) [ "$n" -gt 0 ] && channel=$1 ;;
      --channel=*) channel=${a#*=} ;;
      --uninstall) uninstall=1 ;;
      --purge) purge=1 ;;
      --verbose) verbose=1 ;;
    esac
    set -- "$@" "$a"
  done

  root=${CLAWDLINE_NEXT_INSTALL_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/clawdline-next}
  openssl=${CLAWDLINE_INSTALL_OPENSSL:-openssl}

  if [ "$uninstall" = 1 ]; then
    [ -x "$root/current/clawdline" ] && exec "$root/current/clawdline" setup "$@"
    # Nothing to run `setup --uninstall` with: an earlier uninstall took the
    # binary. --purge still owes the state directory, so it is removed here.
    say "Clawdline is not installed at $root; nothing to stop or unlink."
    [ "$purge" = 1 ] || exit 0
    purge_state
    exit 0
  fi

  # CLAWDLINE_INSTALL_TEST_KEY is honoured only beside
  # CLAWDLINE_INSTALL_BASE_URL — a person who sets that has already chosen
  # the host — so the one-line install trusts only release_keys.
  if [ -n "${CLAWDLINE_INSTALL_TEST_KEY:-}" ] && [ -z "${CLAWDLINE_INSTALL_BASE_URL:-}" ]; then
    die "CLAWDLINE_INSTALL_TEST_KEY is for tests and is honoured only with CLAWDLINE_INSTALL_BASE_URL. Unset it and run this again."
  fi
  platform
  have tar || die "tar is not installed. Install it and run this again."
  have curl || die "curl is not installed. Install curl (wget cannot keep a download on https) and run this again."
  mkdir -p "$root/staging"
  work=$(mktemp -d "$root/staging/install.XXXXXX")
  # The staging and root directories go too when nothing else is in them.
  trap 'rm -rf "$work"; rmdir "$root/staging" "$root" 2>/dev/null || true' EXIT
  trap 'exit 130' INT TERM

  if [ -n "${CLAWDLINE_INSTALL_BASE_URL:-}" ]; then
    base=${CLAWDLINE_INSTALL_BASE_URL%/}
  elif [ -n "$version" ]; then
    base=$repo_url/releases/download/$version
  elif [ "$channel" = beta ]; then
    tag=$(newest_tag)
    [ -n "$tag" ] || die "no release is published yet at $repo_url/releases. Try again later, or pass --version vX.Y.Z."
    base=$repo_url/releases/download/$tag
  else
    base=$repo_url/releases/latest/download
  fi

  say "Downloading Clawdline for ${os}/${arch}…"
  detail "from $base"
  for f in manifest.json manifest.sig.json manifest.sshsig; do
    status=0
    case $f in manifest.json) bound=65536 ;; *) bound=16384 ;; esac
    fetch "$base/$f" "$work/$f" "$bound" || status=$?
    # A release from before the SSH copy of the signature has none; openssl
    # checks manifest.sig.json, which every release has.
    if [ "$f" = manifest.sshsig ] && [ "$status" = 22 ]; then
      rm -f "$work/$f"
      continue
    fi
    if [ "$status" = 22 ] && [ -z "$version" ] && [ -z "${CLAWDLINE_INSTALL_BASE_URL:-}" ]; then
      die "no release is published yet at $repo_url/releases (the latest one has no $f). Try again later, or pass --version vX.Y.Z."
    fi
    [ "$status" = 0 ] || die "could not download $base/$f. Check your network and run this again, or pass --version vX.Y.Z."
  done

  # Nothing of the manifest is read before its signature is checked.
  verify_release
  say "✓ release signature checked"

  # The manifest is what tools/release wrote, one field to a line; split at
  # braces and commas, a compact one reads the same.
  tr '{},' '\n\n\n' <"$work/manifest.json" >"$work/manifest.lines"
  v=$(field version)
  case $v in v[0-9]*) ;; *) die "the signed manifest names no version (${v:-none}). Nothing was installed; report it at $repo_url/issues." ;; esac
  [ -z "$version" ] || [ "$v" = "$version" ] ||
    die "asked for $version and the signed manifest at $base is $v. Nothing was installed; check the version at $repo_url/releases."

  # Without --version this is "the newest release": only a recent one is.
  if [ -z "$version" ]; then
    committed=$(field committed_at)
    case $committed in
      [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*) ;;
      *) die "the newest release, $v, carries no committed_at to tell its age by, so it is not taken as the newest. Nothing was installed; pass --version $v to install it on purpose." ;;
    esac
    age=$(($(days_of "$(date -u +%Y-%m-%d)") - $(days_of "${committed%%T*}")))
    [ "$age" -le "$max_manifest_age_days" ] ||
      die "the newest release the host serves, $v, was committed $age days ago, more than the $max_manifest_age_days days a newest release may be; the host may be serving an old release in place of a newer one. Nothing was installed; pass --version $v to install it on purpose."
  fi

  # A release older than the installed one is installed only when asked.
  installed=$(readlink "$root/current" 2>/dev/null || true)
  installed=${installed#releases/}
  case $installed in
    v[0-9]*)
      if [ "$downgrade" = 0 ] && older "$v" "$installed"; then
        die "$v is older than the installed $installed; nothing was changed. Pass --allow-downgrade to go back to $v on purpose."
      fi
      ;;
  esac

  suffix="_${os}_${arch}.tar.gz"
  name="clawdline_${v}${suffix}"
  # The archive's sha256 is the one the signed manifest gives that name: the
  # next sha256 after it, as the fields are written in that order.
  want=$(awk -v n="\"$name\"" '
    function val(s) { sub(/^[[:space:]]*"[a-z0-9_]*":[[:space:]]*/, "", s); sub(/[[:space:]]*$/, "", s); return s }
    found && /^[[:space:]]*"sha256":/ { s = val($0); gsub(/"/, "", s); print s; exit }
    /^[[:space:]]*"name":/ && val($0) == n { found = 1 }' "$work/manifest.lines")
  case $want in
    [0-9a-f]*) [ "${#want}" = 64 ] || want="" ;;
    *) want="" ;;
  esac
  [ -n "$want" ] || die "release $v has no archive for $os/$arch. Pass --version with a release that has one ($repo_url/releases)."

  fetch "$base/$name" "$work/$name" 536870912 || die "could not download $base/$name. Check your network and run this again."
  got=$(sha256_of "$work/$name")
  [ "$got" = "$want" ] || die "$name has sha256 $got, the signed manifest says $want; nothing was installed. Run this again; if it repeats, report it at $repo_url/issues."
  detail "$name matches the signed manifest"

  # Unpacked beside its final place and renamed in, so a release directory is
  # whole or absent. setup proves every file matches the signed archive.
  mkdir "$work/unpacked"
  # GNU tar names every macOS extended attribute it skips; those lines say
  # nothing about the files, anything else it says is shown.
  tar -xzf "$work/$name" -C "$work/unpacked" 2>"$work/tar.err" || { cat "$work/tar.err" >&2; die "could not unpack $name. Check the disk has room and run this again."; }
  grep -v 'Ignoring unknown extended header keyword' "$work/tar.err" >&2 || true
  [ -x "$work/unpacked/clawdline" ] || die "$name holds no clawdline binary. Nothing was installed; report it at $repo_url/issues."
  mkdir -p "$root/releases"
  dest=$root/releases/$v
  if [ -e "$dest" ]; then
    mv "$dest" "$work/previous"
  fi
  mv "$work/unpacked" "$dest"

  detail "unpacked $v into $dest"
  status=0
  "$dest/clawdline" setup --archive "$work/$name" --manifest-dir "$work" "$@" </dev/null || status=$?
  # A repair that failed puts back the copy of this version that was there.
  # A first install that setup refused before `current` named it (a bad
  # signature; no tmux in a build that carries none) takes the unpacked
  # release away again, so "Nothing was installed" is true.
  if [ "$status" != 0 ] && [ -e "$work/previous" ]; then
    mv "$dest" "$work/refused" && mv "$work/previous" "$dest"
  elif [ "$status" != 0 ] && [ "$(readlink "$root/current" 2>/dev/null)" != "releases/$v" ]; then
    mv "$dest" "$work/refused"
    rmdir "$root/releases" 2>/dev/null || true
  fi
  exit "$status"
}

main "$@"

# Releasing

A release is what a person who never cloned this repository installs and updates: signed
archives on a GitHub Release of `sainteye/clawdline`. This page is how one is cut, checked and
published, and how the signing key is kept. What a machine does with a release is
[updates.md](updates.md); the install itself is [user/install.md](user/install.md).

## What a release is

| Asset | Holds |
| --- | --- |
| `clawdline_<v>_<os>_<arch>.tar.gz` (linux amd64/arm64, darwin arm64/amd64) | `clawdline` and `dist/` (the local console, with `BUILD.json`) at the archive root — the layout `~/.local/share/clawdline-next/current` points at |
| `Clawdline-<v>-macos-arm64.tar.gz` | `Clawdline Next.app` (tar, so modes, symlinks and the signature survive) |
| `SHA256SUMS` | manual checksum comparisons and compatibility with older installers; not a trust root |
| `manifest.json` | version, commit, `committed_at`, channel, `min_version`, `non_additive_migration`, and each artifact's os, arch, kind, URL, size and sha256 |
| `manifest.sig.json` | a list of `{key_id, sig}` Ed25519 signatures over `manifest.json`'s exact bytes |
| `<archive>.spdx.json` | the SPDX 2.3 SBOM of each archive (`tools/release/sbom.sh`) |

Every asset also carries a GitHub artifact attestation of its build provenance, and the macOS
binaries and app are notarized. What that evidence claims, how anyone verifies it, and what it
does not prove is [security/release-evidence.md](security/release-evidence.md). The manifest
schema is unchanged: the SBOMs are bound to the signed archives by the digest each one names, and
to the workflow by their attestation, so older installers and updaters read releases as before.

A daemon follows a manifest only when one signature in the list is by a key compiled into it
(`internal/adapters/release/keys.go`), and installs an artifact only when its size and sha256 are
the manifest's. The current `install.sh` verifies the manifest with trusted local OpenSSL 3
and its pinned release public keys, parses it with Python 3, and checks the signed archive size
and hash before unpacking or executing the release. The script's pinned keys must match
`internal/adapters/release/keys.go`; the installer policy test holds that trust set and its
copied release bounds to the Go reader and capacity register. A key rotation ships both keys
in both places before an old key is removed.

First installation still trusts the reviewed installer script and local tools. A compromised
release host cannot substitute an unsigned archive, but a compromised installer or signing key
remains a trust failure. Older installer revisions executed the downloaded binary before it
checked its own signature; fixing `main/install.sh` does not change those historical scripts.

Versions are `vX.Y.Z` on the `stable` channel and `vX.Y.Z-beta.N` on `beta`. The stable pointer is
`https://github.com/sainteye/clawdline/releases/latest/download/manifest.json`, which GitHub serves
from the newest published non-prerelease. `min_version` is the oldest version a release may be
upgraded from and rolled back to; a release whose store change an older binary cannot read sets
`non_additive_migration` and raises it.

The README and install guide deliberately pin both the installer source commit and a published
release version. When promoting a later release, update those examples together after verifying
the chosen public installer revision and the signed release manifest. The installer revision
may be newer than the program release; do not present them as the same source tree.

## Cutting a release

1. Make sure `main` is green in CI and the commit is the one you mean.
2. Run the **Release** workflow (`.github/workflows/release.yml`) from `main` with the version.
   It waits for approval of the `release` environment, which holds the signing key, then tests,
   builds every artifact with `tools/release/build.sh` (macOS ones signed with the hardened
   runtime and notarized, every archive with its SBOM), signs with `tools/release sign`, verifies
   against the keys compiled into that commit, attests the provenance of every asset with the
   pinned `actions/attest`, checks those attestations with `tools/release/provenance.sh`, and
   creates a **draft** Release. Nothing built
   from this repository is uploaded as an Actions artifact: a public repository's artifacts are
   public. The one exception is the Linux tmux the archives carry: a macOS runner has no Docker,
   so `.github/workflows/tmux.yml` builds it on an Ubuntu runner of each architecture from the
   pinned, hash-checked tarballs in `tools/release/tmux/sources.sh`, checks it is a static binary
   that serves a session, and hands it over as an artifact kept one day. The release job builds
   the darwin tmux itself, and `build.sh --tmux-from` refuses any binary whose sha256 is not the
   one its `tmux.json` records. Each Linux tmux is attested by the tmux job, and the release job
   refuses one without that attestation for its own commit. The same workflow runs alone on every change to
   `tools/release/tmux/`, so a broken build shows in the pull request.
3. On a machine with the private word list (`.git/info/private-words`), run the gate:

   ```sh
   tools/release/gate.sh vX.Y.Z            # download, verify, evidence, unpack, privacy scan
   tools/release/gate.sh vX.Y.Z --publish  # the same, then un-draft it
   ```

   Before the privacy scan the gate refuses any asset the manifest does not account for, checks
   every SBOM against its archive, every asset's provenance attestation (repository, release
   workflow, `refs/heads/main`, the manifest's commit, a GitHub-hosted runner), and, on a Mac,
   each macOS binary's Developer ID signature, hardened runtime and timestamp and the app's
   stapled ticket and Gatekeeper verdict. Any missing or wrong piece fails the gate; there is no
   warning-only mode. A `-test.N` release read with `--dir` skips provenance and the macOS check and
   says so; a `-test.N` version is never gated from a GitHub draft.

   The scan reads every unpacked file and the printable strings of each binary. In a binary only
   a private word or this machine's home directory is a finding: a binary carries every literal of
   its dependencies, which the source-file exemptions were not written for. CI does not run this
   scan; its green result is not publication approval.
4. Install it from the public URL on a clean machine ([user/install.md](user/install.md)).

Release notes come from `docs/release-notes/<version>.md` when that file exists, otherwise from
GitHub's generated notes.

## A local test release

For testing the installer and the updater without the real key:

```sh
go run ./tools/release keygen /somewhere/outside/the/repo/test.key     # prints the public key
tools/release/build.sh v0.10.0-test.1 /tmp/rel/v0.10.0-test.1 --channel beta --no-app \
  --skip-tests --test-key <public key> --base-url http://127.0.0.1:18080/v0.10.0-test.1
go run ./tools/release sign -key /somewhere/outside/the/repo/test.key /tmp/rel/v0.10.0-test.1
(cd /tmp/rel && python3 -m http.server 18080)
```

A test release also carries SBOMs, so `tools/release/gate.sh <v> --dir <dir>` checks them on it.
The evidence checks themselves are tested, with a synthetic release and a stub `gh`, by
`tools/heavy.sh bash tools/release/supply-chain.test.sh`: a missing SBOM, a wrong digest, an
attestation from another repository, workflow, signer or commit, an environment that names another
repository, a `-test.N` draft gated without `--dir`, and a Mach-O payload difference reported as
signature-only must each fail. `tools/check.sh` runs it whenever `tools/release/` or the release
workflows change, and in every full check.

`--test-key` compiles one extra trusted key into those binaries. It is accepted only for a
`-test.N` version, the workflow refuses such a version, and `--no-app` is required, so no
published binary trusts a key that is not in `keys.go`.

## The code-signing certificate

Official macOS releases require a Developer ID Application identity
(`tools/release/codesign.sh`). The earlier self-signed identity preserved some
code requirements but did not preserve WebKit's keychain grant: securityd
rejected the client as not Apple-signed and added a new cdhash ACL entry for
each App replacement (observed 2026-10-17). A release build now fails before
compilation if the selected identity is not Developer ID Application.

- **It is not the release key.** It proves only that two builds are the same program to macOS; the
  Ed25519 key below is what an install trusts an update by.
- **Credential transition.** Replace the release environment's old self-signed
  signing secret with an authorized Developer ID Application certificate and
  private key. Dev builds that must replace the native App need the same
  identity. Verify certificate ownership before exporting or uploading it.
- **Where it lives**: the `release` environment's `CLAWDLINE_CODESIGN_P12_BASE64` and
  `CLAWDLINE_CODESIGN_PASSWORD` secrets, which the Release workflow requires; and, on the
  maintainer's Mac, `<state dir>/signing/codesign.p12` with `codesign.password` beside it (mode
  0600 in a 0700 directory), which `clawdline dev install` signs dev builds with so a dev build
  and a release stay one program. Keep an offline copy of the .p12 and its password.
- **Notarization**: an official build signs every macOS binary with `--options runtime
  --timestamp` and no entitlements, and submits them with `notarytool`
  (`codesign_notarize` in `tools/release/codesign.sh`); the app is stapled. It needs an App
  Store Connect API key in the `release` environment's `CLAWDLINE_NOTARY_KEY_P8_BASE64`,
  `CLAWDLINE_NOTARY_KEY_ID` and `CLAWDLINE_NOTARY_ISSUER` secrets; without them the build fails
  before compiling. Dev builds are neither hardened nor notarized. The gate accepts only the
  release team `83D62P566Q`, fixed in `tools/release/codesign.sh`, and asks Apple online for
  each binary's notarization ticket; changing that team is a reviewed commit
  ([release-evidence.md](security/release-evidence.md#the-release-team-and-changing-it)).
- **Without it**, a build signs ad hoc as before: a clone builds and runs with no one's certificate.
- **Replacing it** changes the identity once: macOS can ask for the keychain
  password and Automation permission once after the transition. Validate two
  successive signed builds on one Mac before claiming prompts are gone.

## The signing key

- **Where it lives**: the base64 Ed25519 seed is the `CLAWDLINE_RELEASE_KEY` secret of the GitHub
  environment `release`, whose required reviewer is the maintainer, so no workflow run reads it
  without that approval. One offline copy is kept by the maintainer. It is never in this
  repository, a shell history or a build log.
- **Creating one**: `go run ./tools/release keygen <file outside the repo>`; put the printed
  public key in `releaseKeys` (`internal/adapters/release/keys.go`), the seed in the environment
  secret, and the file in offline storage; then delete the file.
- **Rotating**: add the new public key to `releaseKeys` and ship a release signed by **both** keys
  (`tools/release sign` adds a signature and keeps the ones already in `manifest.sig.json`). Once
  installs have that release, a later one may drop the old key.
- **If the key leaks**: rotate as above at once, signing with the new key only, and say so in the
  release notes. Installs older than the release that added the new key cannot verify the new
  releases and need the installer run again.

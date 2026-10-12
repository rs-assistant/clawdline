# App update acceptance record — 2026-10-12

This record is for the update prompt and mixed-version acceptance criteria. A green code test is
recorded separately from a release or source installation on an actual platform. The latter is
required before the Board item can be completed.

## Phone update prompt

The React update store was exercised with controlled visibility, focus, back-forward restoration,
and out-of-order read completions. `node --experimental-strip-types --test
console/src/machine/update.test.ts console/src/machine/update-model.test.ts` passed 36 of 36 tests
on the isolated feature tree. The cases cover a short background return, a completed or withdrawn
version in the next daemon answer, a rollback, dismissal per version, and a pre-background answer
arriving after the return. The Settings install action and list banner are hidden while that return
check is pending.

The foreground path now asks `GET /v1/update/fresh` (the separate `update-fresh` Cloud word), while
progress polling uses cached `GET /v1/update`. A controlled release-host test first returned a
signed v0.11.0 manifest to a v0.10.0 daemon, then returned 404: the cached route still reported
`update_available`, while the fresh route returned `unknown`, no latest version, and
`no_release_published`. This is a daemon-and-browser contract check, not a phone run against a
published withdrawal. That end-to-end acceptance observation remains open.

## Old daemon with current Cloud

The hosted console has typed older-machine handling (`machine/NeedsUpdate.tsx` and
`machine/update-model.ts`), and its fixture tests cover `not_implemented`, `unknown_command`,
and older update routes. There is no recorded end-to-end run of an older App/daemon against the
current production Cloud that proves login, pairing, connection, terminal rendering, and command
receipts together. That acceptance criterion remains open.

## Installation matrix

The host used for this record is macOS arm64. `docker info --format
'{{.OSType}}/{{.Architecture}}'` answered `linux/aarch64`; `wine --version` answered `wine-10.0`.
Neither command performs an installation. The release builder's target loop in
`tools/release/build.sh` produces Linux amd64/arm64 and macOS arm64/amd64 archives plus an optional
macOS App bundle. It produces no Windows release. `cmd/clawdline/update.go` applies a source build
only on Linux, prints manual bundle instructions on macOS, and refuses other operating systems.

| Platform | Release version, entry, post-update | Source version, entry, post-update |
| --- | --- | --- |
| macOS | v0.10.0 release install, signature check, daemon start, console HTTP 200 and clean uninstall passed in an isolated root; version transition and App bundle still untested | Not yet exercised on an isolated installation |
| Linux | v0.10.0 arm64 release archive checksum, extracted binary, daemon start, console HTTP 200 and matching BUILD stamp passed in an Ubuntu 24.04 container; systemd installation and version transition untested | Not yet exercised in a Linux runtime |
| Windows | No release artifact or installer exists; cannot exercise this path | No supported source update entry; cannot exercise this path |

No matrix cell is counted complete. Compilation or a unit fixture is not an installation result.
An isolated runtime check must record the version before and after, the exact download or install
entry, and the post-update console and daemon state for each cell.

The macOS arm64 run used `sh install.sh --version v0.10.0 --headless --no-app
--no-autostart --no-session-check --port 63578 --verbose` with separate install, binary, state,
and App paths under `/private/tmp`. Its signed archive identified commit `bc644851fcdbe0a46741a7f4777a1c1247a2a81f`;
the installed console answered HTTP 200 and its `BUILD.json` named the same commit. The daemon
reported v0.10.0 after installation. The attempt to use `clawdline update --apply --version
v0.10.0 --force` as a transition test was refused with `already_current`, as the updater is
designed to refuse the running version even with `--force`. That attempt does not establish a
post-update state. The test service and LaunchAgent were removed by `setup --uninstall`. The
previous public tag v0.8.0 has only the retired Swift App archive, so it cannot provide an older
Go daemon installation for this release-to-release transition.

The Linux aarch64 run downloaded the public `clawdline_v0.10.0_linux_arm64.tar.gz` and
`SHA256SUMS`; the archive's listed SHA256 passed. In an isolated Ubuntu 24.04 container, the
archive's `clawdline serve --port 7727` answered HTTP 200 on loopback. Both `clawdline version
--json` and the served distribution's `BUILD.json` named v0.10.0 and commit
`bc644851fcdbe0a46741a7f4777a1c1247a2a81f`. The container was stopped after the check.
This uses the release archive directly; it is not a `systemd --user` install, does not exercise
`install.sh` there, and has no before/after version transition.

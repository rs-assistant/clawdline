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

This establishes browser behavior **given a correct daemon answer**. It does not establish an
immediate withdrawal at the release host: `GET /v1/update` reads the daemon's cached signed
manifest, which the daemon refreshes every six hours plus jitter. A browser reopening between
daemon checks can still receive an outdated `update_available` answer. The acceptance criterion
for a withdrawn release remains open.

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
| macOS | Not yet exercised on an isolated installation | Not yet exercised on an isolated installation |
| Linux | Not yet exercised in a Linux runtime | Not yet exercised in a Linux runtime |
| Windows | No release artifact or installer exists; cannot exercise this path | No supported source update entry; cannot exercise this path |

No matrix cell is counted complete. Compilation or a unit fixture is not an installation result.
An isolated runtime check must record the version before and after, the exact download or install
entry, and the post-update console and daemon state for each cell.

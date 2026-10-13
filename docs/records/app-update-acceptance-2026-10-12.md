# App update acceptance record — 2026-10-12, continued 2026-10-13

This record separates a controlled test from an observation on a person's phone or a live
mixed-version Cloud connection. The Board item's revised installation scope is macOS and Linux.
Windows release and source updates are separate work and do not gate this item.

## Phone update prompt

The React update store was exercised with controlled visibility, focus, back-forward restoration,
and out-of-order read completions. `node --experimental-strip-types --test
console/src/machine/update.test.ts console/src/machine/update-model.test.ts` passed 36 of 36 tests
on the isolated feature tree. They cover a short background return, a completed or withdrawn
version in the next daemon answer, rollback, dismissal per version, and a pre-background answer
arriving after the return. The Settings install action and list banner stay hidden while the
return check is pending.

The foreground path asks `GET /v1/update/fresh` (the separate `update-fresh` Cloud word), while
progress polling uses cached `GET /v1/update`. A controlled release host first returned a signed
v0.11.0 manifest to a v0.10.0 daemon, then 404: the cached route still reported
`update_available`, while the fresh route returned `unknown`, no latest version, and
`no_release_published`. These are browser and daemon contract checks. A real phone reopen,
background return, completed update, and published withdrawal have not been observed together;
the phone acceptance step remains open.

## Old daemon with current Cloud

The hosted console has typed older-machine handling (`machine/NeedsUpdate.tsx` and
`machine/update-model.ts`), and fixture tests cover `not_implemented`, `unknown_command`, and
older update routes. An isolated old daemon and a current Cloud console have not been paired and
run through account login, machine pairing, connection, terminal rendering, and command receipts
together. That acceptance step remains open; local fixtures do not prove the deployed relay or
the person's account flow.

## Installation matrix

The 2026-10-13 host was macOS arm64; Docker reported Linux arm64. Both release transitions used
two local beta archives, `v0.10.1-test.1` at `e1e7be06d0bba308827d4f0cc2455ca0ddfab629`
and `v0.10.2-test.1` at `52675f7f398bb2acc5554bd01ba64b3c9d5adee0`. They were signed with
an ephemeral test key compiled only into these test binaries, served over loopback, and never
published. Each archive and manifest passed `tools/release verify` with that key. This tests the
real install and updater mechanisms; it is not a second public release or a production-key
rotation.

| Platform and path | Version decision and install entry | Post-update observation |
| --- | --- | --- |
| macOS release | Public v0.10.0 archive and `SHA256SUMS` passed in an isolated root. Then `install.sh --version v0.10.1-test.1 --channel beta --headless --no-app --no-autostart --no-session-check` fetched and verified the local signed release; `clawdline update --apply` selected v0.10.2-test.1. | The isolated LaunchAgent restarted. CLI reported `apply.state=healthy` and `state=current`; `current` linked to v0.10.2-test.1, its binary and authenticated `BUILD.json` named `52675f7f`, and `GET /` returned 200. |
| macOS source | An `e1e7be06` source daemon reported `source_checkout` and `update_available` against the then-current Cloud `52675f7f`. `update --apply` printed its source-worktree and `tools/package-macos.sh` commands. That script built and installed a signed App bundle at `52675f7f` in an isolated Apps directory. | The App-bundled daemon restarted on the same isolated state and port. Its binary and served `BUILD.json` named `52675f7f`; `GET /` returned 200; the fresh check then reported `current`. The bundle carried tmux 3.6a. |
| Linux release | In a Linux/arm64 container with a real `systemd --user` manager, the same `install.sh` invocation fetched the local signed v0.10.1-test.1 archive and installed its user unit. `clawdline update --apply` fetched and verified v0.10.2-test.1. | The user service restarted and `apply.state=healthy`, `state=current`. The active arm64 binary, `current` symlink, and authenticated `BUILD.json` named v0.10.2-test.1 / `52675f7f`; `GET /` returned 200. |
| Linux source | The service user's `tools/deploy-linux-user.sh --stage-only --rev e1e7be06…` built the first source release, followed by the one-time user-service bootstrap. `tools/deploy-linux-user.sh --rev 0361cd8d…` then built and switched to the corrected `origin/main` commit. | `systemd --user` was active with a new daemon PID. The new binary reported `linux/arm64`, the `current` symlink and authenticated `BUILD.json` named `0361cd8d2f70a4386c69f89f0c93461a8b45a943`, and `GET /` returned 200. The update check said `ahead` because production Cloud still named the older `5ae09562` stamp at that instant. |

The public v0.10.0 macOS App archive also passed its published SHA256 and `codesign --verify
--deep --strict` after extraction. Its App-bundled binary and web `BUILD.json` both named
`bc644851fcdbe0a46741a7f4777a1c1247a2a81f`. The shell was not opened as a GUI app in this
run; the source-built App bundle's daemon and console were run instead.

The Linux source run exposed three installation defects before the successful transition. On
arm64, the builder forced GOARCH=amd64, so Docker's emulation hid a failure a native machine
would have; it also used `rg` for the CloudGate guard although `rg` was absent, silently skipping
that guard. A fresh bootstrap read `local-token` before the daemon created it and exited despite
the user service starting. Commit `0361cd8d` selects the host architecture, uses `grep`, and
polls for the token within the existing health deadline. The delayed-token bootstrap test and
the repository's full `tools/check.sh` passed; the successful Linux source deploy above used
that commit. Initial container test failures were separately traced to absent `ss` and shell
prompt scheduling under emulation. Installing the standard system tools, setting a UTF-8 locale,
and running Go packages serially let the unchanged product tests pass.

These four cells record version decisions, actual download or install entries, and post-update
daemon and console states on macOS and Linux. The Board item remains open for the phone and live
old-daemon/new-Cloud criteria above.

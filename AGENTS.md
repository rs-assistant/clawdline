# Working in this repository

**This is the product.** Clawdline is a Go daemon plus a React console, built for macOS, Linux and
Windows. The daemon owns port 7727 on this machine. `README.md` says what it is for; `docs/` says
how each part was decided. This page holds the rules; `docs/working-rules.md` holds the incidents
and procedures behind them — read its section when you reach the thing it covers.

## This fork: rs-assistant/clawdline

This fork runs Clawdline **only from source we have reviewed**. It is not a release install and
does not take upstream releases or self-updates. Open items for the next session are in
`handoff.yaml`; everything durable is here.

### Repository and branch

- Fork `rs-assistant/clawdline`, upstream `sainteye/clawdline`. Remote names depend on the
  checkout: a cloud-container clone has the fork as `origin` and no upstream remote. Read
  `git remote -v` before fetching or pushing; to compare with upstream,
  `git remote add upstream https://github.com/sainteye/clawdline`.
- Work happens on `local-source-build`, cut from upstream `418bac7` (security-reviewed on
  2026-10-19). Merging upstream brings unreviewed code: review the diff against the last reviewed
  commit and rerun govulncheck before merging.
- The person wants Claude and Codex to cross-review changes. Present their disagreements instead
  of resolving them silently.

### Build and run (source build only)

```sh
(cd web && npm ci --ignore-scripts && npm run build)   # lockfile integrity, no install scripts
go build -o bin/clawdline ./cmd/clawdline              # go.mod pins toolchain go1.27.3
CLAWDLINE_NEXT_UPDATE_CHECK=off \
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" \
./bin/clawdline serve
```

- Do not use `install.sh` or `clawdline update`. A source build never starts the GitHub release
  updater (`internal/transport/http/update.go`, `kind != install.KindRelease`). `install.sh`
  checks only a same-origin SHA-256 and then runs the downloaded binary, which verifies its own
  signature; verifying release assets with `go run ./tools/release verify` still installs
  upstream-built binaries and turns the release updater on. Both were rejected on 2026-10-22.
- `CLAWDLINE_NEXT_UPDATE_CHECK=off` is required: without it the daemon GETs
  `app.clawdline.com/BUILD.json` every 30 min (`internal/adapters/updatecheck`, checked in `Run`
  and `Refresh`).
- After a dependency change, run `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` and
  `(cd web && npm audit)`. Third-party Go and npm dependencies are hash-verified, not
  code-reviewed. Pick fixed versions that have been published for at least two weeks.

### Operating policy

- Cloud stays off (`cloud_enabled` unset or false). Do not run `clawdline cloud login`.
  `clawdline cloud off` stops a live relay line on a daemon built from this branch after the
  2026-10-22 fix; an older build keeps the line up until the daemon restarts.
- Do not enable `remote_tunnel`.
- Dispatch with `--permission-mode ask`, the default on this branch. `full` is
  `bypassPermissions` / `--ask-for-approval never`. Never add `clawdline` to an agent's Bash
  allowlist.
- Leave `documents_contain_project_root` unset or `true` (contained is the default on this
  branch): `false` lets an `artifacts` symlink widen what a read-only device can read.
- Do not open untrusted services on other `127.0.0.1` ports in the browser signed in to the
  console. Browsers do not scope cookies by port, so they receive the session cookie; on Linux the
  daemon refuses it from another user's process, not from a process of the person's own.
- Before handing off a Secret Handoff value, read the command the Note shows: that program gets
  the value. A shell, curl or an unfamiliar address is a reason to stop.

### Dependency changes on this branch

- 2026-10-22, Go: `golang.org/x/net` v0.50.0 → v0.60.0 and `toolchain go1.27.3`. govulncheck had
  found 13 reachable vulnerabilities (x/net HTTP/2 and DNS, stdlib in go1.27.1); after the bump, 0.
  It raised the `go` directive 1.25.0 → 1.26.0, `x/crypto` to v0.57.0 and `x/sys` to v0.48.0;
  `go mod tidy` made goldmark and `x/crypto` direct; the README badge and `docs/user/install.md`
  say Go 1.26.
- 2026-10-22, npm: `npm audit` found 2 high, both build-time only: vite 6.4.3 (five advisories in
  the dev server and launch-editor) and source-map-js 1.2.1 (DoS from a crafted source map,
  through postcss). Bumped to vite 6.4.4 and source-map-js 1.2.2 (`e496fa9`); `npm audit` then
  found 0.

### Security review

Static review of `418bac7` on 2026-10-19: Claude (five subagents) and Codex (two runs), verdicts
spot-checked against the source. Every finding below was fixed on this branch on 2026-10-22, each
with a test that failed before its fix, except where "Still open" says otherwise. The fixes are
not cross-reviewed; the person dropped the Codex cross-review.

First round, `8d4bf41` and `5c8ca6c`:

- Medium: dispatch defaulted to `full`. A brief or CLI call that names no mode now runs `ask`
  (`internal/app/orchestrator/draft.go`, `cmd/clawdline/dispatch.go`).
- Medium: `clawdline cloud off` wrote the file first, so the route found no change and left the
  relay line up (`internal/transport/http/cloud_onboarding.go`). It now compares with the live line.
- Medium (Codex only): `/v1/events` stayed open after device revocation. The stream now
  re-verifies its credential on every tick and heartbeat (`gate.go` `stillAdmitted`, `stream.go`).
- Medium (Codex only): an `artifacts` symlink widened document reads. Containment is now the
  default (`internal/transport/http/documents.go` `containsProjectRoot`).
- Low: console pages had no `frame-ancestors`. `page.go` now sends
  `Content-Security-Policy: frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff` and
  `Referrer-Policy: no-referrer` (a link out no longer names `127.0.0.1:7727` to the site).
- Low: an unvalidated pre-release segment in a version string could name a path.
  `ParseVersion` now accepts only semver identifiers.

Second round:

- High, `181ab79`: unpinned senders fell back to the Cloud roster key. Only browsers in this
  machine's own pin file are admitted (`link.go` `publicKeyFor`, `authority`,
  `terminalAuthority`, `transcriptAuthority`); the roster can still deny but never supplies a key.
- High, `181ab79`: revoking a device did not replace the master secret. `clawdline cloud revoke`
  and `POST /v1/cloud/devices/revoke` now save a new secret and rebuild the line, and name every
  browser that must pair again; a revocation that could not replace the key answers 500
  `secret_not_replaced`.
- Medium, `181ab79`: no envelope timestamp check, replay window in memory. An envelope whose
  signed `ts` is more than 5 min from this machine's clock is dropped (`clock_skew`); accepted
  claims go to `cloud/replay-journal-v1` (0600) before the envelope is opened, so a reconnect or
  restart does not reopen the window (register rows `cloud.replay_journal_*`).
- Disputed (Codex High, Claude Low, assessed Medium), `181ab79`: `ctl/` reads skipped
  re-authorization. `serveRead` now re-checks the same authority as writes.
- Medium, `d8b1cf2`: Secret Handoff fed the value to any command the agent chose. The Note now
  declares the exact argv (absolute executable); the console shows it beside the field and repeats
  it in the submission; a claim with any other argv gets nothing, the waiting value is wiped and
  the Note closes as refused.
- Medium, `125bb2d`: the session cookie is not port-scoped. On Linux, a cookie, bearer,
  orchestrator token or task secret arriving from a loopback socket another uid owns is refused
  (`peer.go`, `internal/adapters/sockowner`); an unreadable `/proc` refuses local credentials.
- Low, `125bb2d`: on multi-user Linux, every CLI call checks that its loopback peer is its own
  uid before writing a credential (`listenerowner.go`, on `http.DefaultTransport`; the daemon is
  the same binary, so its own loopback calls to another user's socket are refused too);
  `clawdline open` passes a one-time five-minute code instead of the device token;
  `clawdline pair --reset` (local token only) reopens pairing that wrong codes closed.
- Medium (Codex High), `cbc74e4`: `install.sh` ran the downloaded binary before an independent
  signature check. It now verifies `manifest.json` against the embedded release keys with OpenSSL
  3 or `ssh-keygen -Y verify` (new `manifest.sshsig` asset) before anything runs, and refuses when
  neither verifier exists. wget is no longer used (its `--https-only` did not stop http
  redirects); curl is https-only.
- Low, `cbc74e4`: forced downgrade and non-expiring manifests. `install.sh` refuses an older
  release without `--allow-downgrade` (the updater already needed `--force`); a manifest whose
  signed `committed_at` is missing or older than 180 days is not "the newest release"
  (`release.manifest_age_seconds`). A version named on purpose still installs.

What the hosted console (deployed by the operator from upstream) loses against this daemon:
browsers only on the account roster cannot reach it until `clawdline cloud pair` pins them; after
a revocation every other browser stops decrypting until it pairs again, with no prompt; envelopes
stamped more than 5 min off are dropped silently; `ctl/` reads fail while the roster is
unreadable; a hosted console built before `d8b1cf2` cannot hand off a Secret Handoff value.

Still open:

- High: the hosted console JS from `app.clawdline.com` holds the keys and cannot be verified
  against the repository. No daemon-side change can fix it; it matters only with Cloud on.
- A revocation made in the hosted console (on the account roster) denies the device but does not
  replace the key; devices revoked before `181ab79` are not re-keyed by revoking again. The CLI
  viewer and `clawdline cloud connect` keep an in-memory replay window. Replay-journal appends are
  not fsynced per envelope.
- The cookie still reaches a service of the person's own user; the socket-owner checks do not
  exist on macOS or Windows.
- A declared Secret Handoff command that is itself a shell or curl still receives the value: the
  person's check of the shown command is the protection.
- Whoever keeps guessing pairing codes can close pairing again after a reset (five guesses each).
- Releases published before `cbc74e4` have no `manifest.sshsig`, so a Mac without OpenSSL 3
  cannot use `install.sh` until a newer release exists. Stable releases must follow each other
  within 180 days. The beta channel's tag list is unsigned. `clawdline setup` from an older
  unpacked release, and `--force`, still downgrade on an explicit ask. Not run on macOS.
- Browser end-to-end tests for the Secret Handoff console were updated but not run (no browser in
  the container).

Found sound: no telemetry, analytics or crash SDKs; local HTTP has CSRF, Origin and Host checks;
no exploitable XSS; no `sh -c` injection; the release updater verifies the ed25519 signature
before parsing; token and key files are 0600.

### What leaves the machine

- With a source build, Cloud off and the update check off, nothing goes to the Clawdline operator
  or to GitHub. What remains, none of it to the operator: AI auto-naming sends conversation
  snippets and paths to Anthropic or OpenAI through the local `claude` or `codex` CLI; browser
  push subscriptions go to Apple or Google push services, end-to-end encrypted; documentation and
  plan links open `clawdline.com` or `app.clawdline.com`, which then see the IP but no referrer.
- With Cloud on, the operator sees the hostname as the machine name (renameable), the OS, the
  version and the account. The relay sees the machine id, session ids, timing, sizes, and which
  device reads which session. Titles, projects, transcripts and terminal content are end-to-end
  encrypted, but `app.clawdline.com` serves the JS that decrypts them. Cloud direct uses Google
  and Cloudflare STUN, which see the public IP.
- Local collection: the daemon scans every `~/.claude/projects/*.jsonl` and `~/.codex/sessions`
  file, including sessions Clawdline did not start, for token counts only (kept 57 days, no
  opt-out). It does not read Claude or Codex credential files; only
  `experiments/personas/round5/run.mjs` copies `~/.codex/auth.json`, and no daemon path runs it.
  Voice transcripts persist in idempotency receipts for 24 h. Security audit segments are never
  deleted automatically.

## The retired Swift app

The Swift app stopped on 2026-09-19 and was archived on 2026-09-22; keep its archive read-only and
its file-and-line citations intact (see `docs/working-rules.md`), and let
`TestNothingSaysTheRetiredAppIsStillRunning` guard against claims that it still runs.

## Where work happens

The branch is `main` (it was `master` until 2026-09-22). Work lands through a **disposable
worktree**, never in the shared checkout:

```sh
git worktree add -f <scratch>/<name> HEAD
```

- A root integrates by **merging the child's branch**, not by applying its patch, and records the
  landing as soon as it lands. A branch cut before 2026-09-22 can only be cherry-picked
  (`docs/working-rules.md`).
- After recording a landing, run `tools/check-worktrees.sh` (a dry run: 0 nothing owed, 1 landed
  residue, 2 could not check, 3 unknown). Inspect it, then run `--apply` for every proved
  `landed_clean` checkout. Remove a root-owned task worktree after its commit is on `main`, its
  checkout is clean, and no owner is using it. Never use age or ancestry alone to delete an
  unknown, active, dirty, or unlanded checkout. In every delivery, tell the person how many
  worktrees were removed and which candidates were kept, with their reasons. For a read-only
  throwaway checkout use `tools/check-worktrees.sh --ephemeral -- <command>`. `docs/worktrees.md`.
- A dispatched child's worktree path comes from the task record
  (`GET /v1/orchestrator/tasks/<id>` → `.task.worktree.path`), never composed.
  `cd "$W" || exit 1` — a failed `cd` runs everything else somewhere else.

## Before anything is committed

The workflow targets **ten seconds during development and one minute for an ordinary local
commit**. Run the smallest relevant package/test while editing with
`tools/check.sh --quick ./package --run=TestName`. This is never an integration receipt.

**`tools/check.sh` is the local commit gate.** It runs directly edited or paired Go test modules (whole-package fallback), affected console tests,
incremental whole-project type checking, changed-file privacy and repository guards. `--plan`
explains the direct scope, module selectors, guard triggers and transitive callers; `--callers` also compiles those callers' tests.
Caller runtime suites, cross-platform builds/vet, complete suites and bundles run in background
CI after integration. They do not block a local commit, landing on main or a dev installation.
A public Release requires complete verification of its exact tree and release acceptance.
Dev installation builds the selected commit, then checks the running version, console and existing
Sessions; a failed startup rolls back. Report pending or failed CI explicitly and fix failures.
Unknown/tooling/policy inputs and unreliable scope discovery conservatively select full checks
when requesting a local check, but are not a dev-install prerequisite. Platform inputs remain
named in the plan; foreign-platform validation belongs to CI and the Release gate.

Every selected check must be green; unknown is not a pass. **`tools/check.sh --full` runs every
check below**, once inside `tools/heavy.sh`, and prints individual outcomes and elapsed times.
`--full --profile` retains uncached Go timing evidence. Keep new checks in that script too.
See [docs/checks.md](docs/checks.md) for coverage, timing, fallback and cache ownership semantics.

```sh
gofmt -l internal cmd          # silent
tools/heavy.sh go vet ./...
tools/heavy.sh go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 tools/heavy.sh go build -o /dev/null ./cmd/clawdline
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 tools/heavy.sh go build -o /dev/null ./cmd/clawdline
GOOS=windows tools/heavy.sh go vet ./...      # the Windows-only files, and every test, as Windows compiles them
tools/heavy.sh go test ./...
tools/heavy.sh go test -count=1 ./internal/config # uncached repository copy guards
go run ./tools/contract-gen -check      # Go and TypeScript are generated together
tools/check-legacy-css.sh               # the pinned CSS copies still match; legacy JS is owned code
node web/console/tools/check-rounded-side-borders.mjs # no rounded surfaces with accent side borders
tools/check-machine-words.sh            # no shown sentence calls the machine a Mac
tools/heavy.sh bash tools/test-machine-words-changes.sh # delta scanning includes new files and preserves failures
tools/heavy.sh bash tools/test-heavy-handoff.sh # capability discovery accepts help exit statuses
tools/check-shell-expansions.sh         # no $name runs into a non-ASCII character
tools/check-private.sh                  # nothing of the person's is in a public repo
node tools/check-console-tests.mjs      # every console test file is run by npm test or npm run check
node tools/check-mirror-tests.mjs       # no console test asserts on the shape of the code
tools/heavy.sh node --test tools/check-plan.test.mjs tools/check-profile.test.mjs tools/check-scope.test.mjs
tools/heavy.sh bash tools/test-check-modes.sh # entrypoint scope, fallback and failure propagation
tools/heavy.sh bash tools/test-linux-dev-build.sh # Linux only: dev skips full checks; ordinary builds preserve failures
tools/heavy.sh bash tools/test-heavy-linux-cache.sh # Linux only: cache ownership, retention and pressure cleanup
( cd web && npm run check && ../tools/heavy.sh npm run build && ../tools/heavy.sh npm test )   # when anything under web/ changed
```

- **Every compile, test suite or bundle build goes through `tools/heavy.sh`** (`clawdline heavy`).
  Wrap the whole script once; wait for it with one long wait. If it exits 75 before acquiring the
  slot, run it later. A queued `tools/check.sh` hands itself to a callback and exits 76: a root
  ends its turn and waits for the notice; a child waits in place. See `docs/working-rules.md` for
  the incident and procedure.
- **After compilation, remove the cache you created for that build** once its commands have exited
  and no other process uses it. Confirm the exact path belongs to this task before removing it.
  The persistent Linux heavy-wrapper cache follows its registered budget; per-worktree TypeScript
  build information is reclaimed with the checkout. Neither follows per-command Session cleanup.
  Do not clear the default shared Go cache, another Session's cache, or a broker-managed gate cache
  as routine post-build cleanup. See `docs/disk-lifecycle.md` for ownership and shared-cache rules.

- A test that needs a Unix facility asks for it through a per-platform file
  (`gone_unix_test.go` beside `gone_other_test.go`), not through `syscall` inline.
- A new bound — a limit, a cache size, a number of rows — must be registered in
  `internal/domain/capacity`; `TestEveryBoundIsRegistered` fails otherwise. Drive the guard red
  before you make it green, and keep the output that proves it went red.
- When slow sends, long loading, pending messages that never clear or lost events recur, do not
  only add a timeout or a spinner: trace the whole capacity and protocol path — queue and
  concurrency limits, backpressure, synchronous external dependencies, retry amplification,
  idempotency and receipts, SSE resume, stale snapshots, failure isolation — and tell apart
  accepted, executed, delivered, observed and acknowledged. The fix carries typed errors and
  failure-injection tests.

## This repository is public

**Nothing of the person's may appear in it**: no names of their businesses, no tokens, no paths
into their accounts, no content from their conversations. `tools/check-private.sh` without its word
list answers 3, undetermined, not 0. Automatic checks read the working tree only; historical scans
are manual audits for an initial publication or an intentional history rewrite. A history finding
names the commit, file and line, never the matched text. `docs/privacy-guard.md` is the whole of it.

Commit messages, comments and documentation are in **English**; the conversation with the person
is in Traditional Chinese.

## Keep filesystem searches inside the task's scope

On macOS, a command started through Clawdline can make a privacy prompt name Clawdline even when
the command itself is the reader. Search the named repository or task directory, not the whole home
directory. Do not recursively enumerate `~`, `~/Library`, `~/Documents`, `~/Downloads`, `~/Music`,
or the Photos library to discover files for a repository task. Open a protected location only when
the task actually requires it and its exact path is known. The observed attribution and the
permission prompts it caused are in `docs/working-rules.md`.

## Maintaining product translations

When changing product-generated, person-facing copy, update English and Taiwan Traditional Chinese
in the same change. Other shipped languages may be translated in batches; do not require their
files to change with every code edit. A missing newer translation falls back to English and is
counted in the localization coverage report. Keep placeholders, plural forms, and markup valid in
every existing translation. An outdated translated guide section falls back to its current English
section so it cannot show obsolete commands. The first multilingual release requires a complete
baseline in every shipped language; later routine changes use the two-language gate. The resolver,
fallback and release checks are in [docs/localization.md](docs/localization.md).

## How a commit reads here

The subject is a sentence about what changed for a person, not a category:
`A tmux that escapes the separator is a listing, not an empty machine`. The body says what was
measured, what the machine said that contradicted the first guess, and what was deliberately left
undone. Numbers come from a run, not from memory.

## How every delivery report ends

When a root Session has finished and verified a turn, including any required commit, run
`clawdline session report --summary "One concrete sentence about what was delivered."` as the
last action before the final answer. Confirm that the command succeeded; the final answer and the
status lines below do not create the Session delivery receipt. If the command is refused,
say that the Session status was not updated and give the refusal reason. Do not report a turn that
is unfinished, only diagnostic, blocked, or waiting for the person, and never report a child's turn.
Read `clawdline guide` for the current command and receipt semantics before using it.

The final response for every task ends with exactly five unbulleted status paragraphs in this order:
GitHub, `app.clawdline.com`, this machine's dev install, public Release, and `clawdline.com`.
Put each item on its own line **with a blank line between items** so Markdown renders five
separate rows, never one wrapped paragraph. Start each with its emoji. Add no text, heading,
code fence, or other content after the five items. Choose each status independently for this
delivery; a newer commit in one place does not establish the state of another place. Use these
meanings:

- `✅` — the required action is complete and its destination has been verified.
- `➖` — this delivery requires no action at that destination. Say why briefly; do not use this
  merely because the action has not been attempted.
- `🕰️` — an action is required or underway, or its required verification is still pending.
- `❔` — the evidence needed to decide is unavailable. Name what could not be checked.

Use these line labels and concise, specific details, including a commit or version when relevant:

```text
✅ GitHub：<本次 commit 已在 origin/main；或寫明待推送／無需變更／無法確認>

✅ app.clawdline.com：<已上線並驗證；或寫明待部署／不需更新／無法確認>

✅ 這台 Mac 的 dev：<正在運行哪個 dev commit；或寫明待安裝／不需更新／無法確認>

✅ Release：<已發布哪個公開版本及其 commit；或寫明待發版／暫不需發版／無法確認>

✅ clawdline.com：<官網已更新並驗證；或寫明待更新／不需更新／無法確認>
```

Replace each example `✅` with the emoji warranted by that line's evidence. For GitHub, `✅`
requires this delivery's commit to be reachable from current `origin/main`; a local commit, an
unpushed merge, or a pending/rejected push is `🕰️`. For `app.clawdline.com`, `✅` requires the
production `BUILD.json` to name the delivered commit or a descendant and the served main bundle
to pass the `CloudGate` check in `docs/hosted-console.md`. A queued build, preview deployment,
stale stamp, or skipped online check is `🕰️` when a deployment is required. An unrelated change
that does not need a hosted-console deployment is `➖`.

For this Mac's dev install, compare the intended dev commit with the version actually running on
this machine, not just a built or installed artifact; see `docs/updates.md`. Use `➖` when this
delivery does not call for a dev install, including after a deliberate return to the stable
release. For Release, compare the intended public version and commit with the published signed
release, not with the hosted console's stamp; see `docs/releasing.md`. A dev build ahead of the
public release does not by itself mean a release is due. Before release acceptance, mark a
dependent website download update as `🕰️` if it is already known to be required, and say it
follows the release. For `clawdline.com`, use the website's own deployed version or visible page
and its acceptance evidence; neither this repository's commit nor the hosted console stamp proves
that website was updated. Use `❔` rather than guessing when that evidence is unavailable.

If a delivery changes Cloud API or relay, report their deployment separately before these final
five lines; the hosted console's status does not cover them. Any required review-residue statement,
including `Fixed but not yet released (awaiting review)`, also goes before the final five lines.

## What a child does not do

- `git add -A` in a shared tree, or stage a file it was not assigned.
- Leave its delivery uncommitted: it **commits to its own task branch** and never pushes — the
  commit is what lets the landing be proved.
- Start a daemon on port 7727 or write to the person's running one. Use an **empty**
  `CLAWDLINE_NEXT_DIR` of its own, never a copy of `~/.config/clawdline-next`.
- Read `~/.config/clawdline` except where the code already does, or write it.
- Stop a daemon by pattern: stop your own **by the PID** your `serve` printed.
- Remove a private tmux socket directory while its tmux server still runs: kill that
  server first, or its assistants keep running where nothing can reach them.
- Confirm a restart with `/v1/health` alone: the console is back when `GET /` answers 200.

The incident behind each of these is in `docs/working-rules.md`.

## Questions that need the person to choose

For a concrete choice the person must make, read `clawdline guide note`, create one `answer` Note
with complete suggested replies, then ask briefly in chat and wait for the person's sent message.
For Codex's non-blocking `request_user_input_async`, also create one answer Note with the same
question and complete replies so the person can return to it later. A blocking question screen
(Claude Code's `AskUserQuestion`, or a blocking Codex prompt) needs no duplicate Note. Suggested
or preselected options are not answers; wait for the person's sent message.

## The single pages

| Question | Page |
|---|---|
| What is the architecture | `docs/architecture.md` |
| Why a decision is what it is | `docs/design-decisions.md` |
| How work is tracked: board, to-dos, Backlog, issues | `docs/work-system-v2.md`, `docs/work-system.md` (v1 record) |
| What each platform does and does not do | `docs/cross-platform.md`, `docs/linux.md` |
| The broker: dispatch, landing, handoff | `docs/broker.md` |
| Cloud and the phone | `docs/cloud.md`, `docs/cloud-wire.md` |
| Deploying app.clawdline.com | `docs/hosted-console.md` |
| Every bound and where it is enforced | `internal/domain/capacity` |
| What keeps the person out of a public repository | `docs/privacy-guard.md` |
| The incidents and procedures behind these rules | `docs/working-rules.md` |

The capacity register implements the policy in `docs/limits.md` §4; the rest of that file is a
dated retired-app record.

Where instruction files conflict, the nearest one wins, and a task brief wins over all of them.

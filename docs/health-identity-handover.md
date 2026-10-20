# Connection health: what the daemon sends after an identity handover

This page is the daemon side of two Cloud uploads that run beside the relay, not through it:
owner-sealed diagnostics (Cloud contract §13) and the producer-reported loss totals of the metadata
uploader (§12.1). The pairing that records a content-key epoch is
`internal/adapters/cloud/health_pairing.go`; this page starts where its record exists.

## Owner-sealed diagnostics (`cmd/clawdline/health.go`)

`startConnectionHealth` builds one `healthsealed.Worker` per boot (`newSealedDiagnostics`) and runs
it on its own goroutine for the daemon's lifetime.

**Consent.** Off by default. A seal or a send happens only while both
`cloud_health_owner_retention` (`healthsealed.SettingKey`) is a JSON `true` and `cloud_enabled` is
on. The metadata switch `cloud_health_upload` never permits it. Both are re-read from the settings
file before every seal and every send, so withdrawing either stops the next request; the journal
writer only reads the worker's cached opt-in (`Worker.Enabled`), never a file.

**Keys and epochs** (`sealedKeys`). For each seal the daemon reads the identity
(`machine-identity-v1.json`), the master secret once (`cloudkeys.Files.MasterSecret`) and asks the
content-key epoch record (`content-key-epoch-v1.json`, `ContentEpochStore.Load`) about that very
secret: the record must name the same key id and account. Anything else — no record, another
account, another secret — is `key_handover_required`, returned before any request is made. No epoch
is assumed. Only then is `GET /v1/machines` asked for this machine's current `identity_epoch`, on
the sealed sender's own client. The master secret is cleared after the key is derived, and the
derived key after sealing; a send never holds the secret.

**Independent of the relay.** The sender has its own HTTPS transport (`sealedClient`: one
connection, 5 s dial and TLS, 10 s per request, no redirects so the bearer goes nowhere else). It
reads only stamped journal records (`sealedFeed`, attached to the watchdog's writer beside the
durable store, and alone when the store cannot open), so a stuck relay socket, writer lock or spool
does not hold it.

**What is sealed** (`sealedJob`). Records that say where the machine was stuck — `slow_detected`,
`stall_detected`, `released_stalled`, and `state_after_loss` in `slow` or `blocked` — become one
attachment per phase (`subject_kind: incident`, `subject_id: phase-<id>`): location, age, deadline,
build, stage, correlation, observed time, and the watchdog's dropped-transition count as coverage.
Nothing else is in the whitelist.

**Lifecycle and failures.** The exact request body is written before the first send. A revision in
flight at shutdown stays pending; the next boot's worker resends it byte for byte on start and every
minute (`Worker.Run`), so a lost reply replays as `duplicate`. A pending revision whose identity
epoch has rotated is refused locally and never sent. Stop waits for the worker within the
connection-health stop deadline and names it if it is late.

| Case | What the status says |
| --- | --- |
| opt-in off | `owner_diagnostics.coverage: off` |
| no recorded epoch / wrong key | `failed`, `key_handover_required`, no request |
| no identity or secret | `failed`, `key_unavailable` |
| Cloud unreachable, 5xx | `failed`, `upstream_unavailable`; the body stays pending |
| jobs dropped at the queue bound or refused | `partial` with the counts |
| receipted | `active` |

`owner_diagnostics` is part of `upload` in `GET /v1/connection-health/status`; the
`cloud.health_sealed_*` capacity rows read the same status (queue depth now, local records as last
counted on the worker's goroutine, unmeasured before the first count).

## Producer-reported loss (`internal/adapters/healthupload`)

Store gaps, storage loss and exclusions are lifetime totals of one progress file. The progress file
now carries a `counter_epoch` (`ue-<24 hex>`), minted with its first write and never changed; a lost
or moved-aside file starts new totals under a new epoch.

The totals travel only in **metadata-only batches** (`events: []`, a `local_loss` block, batch id
`ll-<epoch>-<n>`, the uploader's own boot id), built when the settled totals moved past the last such
batch and there is nothing else to send. Event batches never carry the block: their bodies must stay
what the same records rebuild after a lost progress file, or the rebuilt batch would be
`409 idempotency_conflict` instead of a replay. The block is written into the stored body once and
resent byte for byte. A metadata-only batch that is refused or abandoned is not rebuilt until the
totals move again, so it cannot spend the hour's requests in a loop.

Gap and exclusion reasons are Cloud's allowlists exactly. An exclusion reason Cloud does not list is
sent as `unlisted`; a gap reason it does not list, or a value past 2^53−1, makes the whole block
unsendable rather than a block that would read the missing key as 0.

## Not done here

- The local identity route and console comparison below now write the content-key epoch after confirmed handover.
- No console control sets `cloud_health_owner_retention`; it is settable through `clawdline setting`.
- The owner reader is wired below; real-browser acceptance remains pending.
## Product handover and browser health lifecycle

How a browser gets the key that opens a machine's owner-sealed diagnostics, and when the hosted
page records its own connection health. The cryptography is in
`internal/domain/cloud/health_pairing.go` and `legacy/js/net/cloud-pairing.js`; the Cloud contract
is Cloud's connection-health contract §13.9 (summarized in
`docs/admin-connection-health-contract.md` §9b). This page is the product path around them.

## 1. Why a second handover

The legacy pairing (`#pair=`, `internal/transport/cloud/pairing.go` `Pairing`) leaves the master
secret in the browser only as a non-extractable AES-GCM key, with no content-key epoch. That key
cannot be HKDF input and its epoch cannot be guessed, so a browser paired that way answers
`key_handover_required` for sealed diagnostics. It must take this handover once. Nothing extracts
the old key and nothing assigns it an epoch. The legacy pairing is unchanged and still drives the
relay.

## 2. The person's path

1. On the machine: Settings → Remote → **Diagnostics key** → **Show link**
   (`cloud/health-owner/IdentityHandoverBlock.tsx`). The daemon starts a pairing with Cloud and
   answers a `#pairid=<base64url QR>` link (`IdentityPairingFragmentName`), drawn as a QR.
2. On the browser, signed in to the same account, open the link. The hosted page takes the
   fragment out of the address at once (`takeIdentityPairing`), keeps it in this tab's session
   storage through a sign-in, and shows six digits and the machine key
   (`IdentityHandoverDialog.tsx`). **Codes match** sends the offer; **They differ** sends nothing.
3. The machine's Settings window now shows the same six digits and the browser key. **Codes
   match** there is the only approval: `POST /v1/cloud/pairing/identity/decision` with
   `{"pairing_id","approve":true}`. There is no automatic approval and no Cloud-reachable route.
4. Grant, activate and the machine's attested confirm follow without input. The machine records
   the epoch only from Cloud's grant receipt (`ContentEpochStore`, `content-key-epoch-v1.json`
   beside the key files).
5. The browser keeps the result only after verifying the confirm: one IndexedDB put of
   `{v, accountID, machineID, machineFingerprint, contentKeyEpoch, identityEpoch, pairedAtMs,
   hkdfKey, masterKey}` under `clawdline.health-handover:<account>:<machine>`, both keys
   non-extractable (`storeHealthHandover`, `saveHandover`).

## 3. Local routes (this machine's own token only)

| Route | Answer |
| --- | --- |
| `GET /v1/cloud/pairing/identity` | the run's state |
| `POST /v1/cloud/pairing/identity` | starts a run (replacing one in flight) and answers once Cloud issued the QR |
| `DELETE /v1/cloud/pairing/identity` | cancels; Cloud's row is left to expire |
| `POST /v1/cloud/pairing/identity/decision` | the code decision; `approve` is required |

States: `idle`, `waiting_viewer` (link, `expires_at`), `confirm_code` (`code`,
`viewer_fingerprint`), `handing_over`, `paired` (`viewer_device_id`, `content_key_epoch`),
`denied`, `failed` (`error`: `expired`, `grant_not_recorded`, `no_master_secret`,
`transcript_mismatch`, `cancelled`, `unavailable`, or `cloud_<Cloud code>`), `cancelled`. A
remote reader gets 403; a decision for another pairing id gets 404 `unknown_pairing`; a decision
outside `confirm_code`, including a repeated one, gets 409 `pairing_not_waiting`; a malformed body
gets 400. Bounds: `cloud.identity_pairing_decision_bytes` (1 KiB),
`cloud.identity_pairing_run_seconds` (660 s), `cloud.identity_pairing_poll_attempts` (600).

## 4. The reader's authority

`CloudGate.tsx` installs the owner-diagnostics source from the session at each call: account,
device, the device's Ed25519 private key, and `tokenIdentity` — the `key_epoch` and
`identity_epoch` the last `/v1/tokens/device` mint answered (`tokenIdentityOf`; null until a
token was minted, never 0). `heldMasterKey(machine)` reads only the handover record
(`heldMasterKeyFor`); no record is `{ key: null, contentKeyEpoch: null }`.

What stops or clears it:

- Sign-in screen (sign-out, expired session): the handover in flight stops and opened plaintext
  is cleared. Records stay: they are keyed by account and open nothing for another one.
- Another account: the run stops at its next await (`stale`); a record written after the stop is
  deleted again.
- Revocation of this browser: the run stops, plaintext is cleared, and the account's records are
  deleted (`forgetHandovers`).
- An identity-epoch rotation (any device of the account revoked): Cloud voids the grant, the next
  token mint names the new epoch, and sealed reads are refused until the browser takes the
  handover again. Nothing in the browser assumes the old grant still holds.

## 5. The browser's health producer

Off by default. It runs only in a hosted build made with **`VITE_VIEWER_HEALTH=on`**
(`VIEWER_HEALTH_BUILD_FLAG`), only while the page is signed in to the Cloud, attributed to the
machine being read (`ViewerHealthLifecycle`, `health-viewer/lifecycle.ts`). It stops on the
sign-in screen, revocation, another account (a fresh producer, not the old buffer) and unmount.
No account setting exists for it today; a build flag is the one explicit opt-in the current
architecture has, and no deployed page records anything unless its build sets it. What it records
and what it cannot record (`viewer_sse_gap`, the direct carrier) is in
`docs/admin-connection-health-contract.md` §11.

## 6. Proof

- `internal/transport/http/cloud_identity_pairing_test.go`: local-only routes, required decision,
  cancel, approve and deny.
- `web/console/src/cloud/health-owner/handover.test.ts`: the persistence seam — kept only after
  confirm, reload into an HKDF key for the right epoch only, nothing after denial, stop, account
  switch during the write, or revocation; the `#pairid=` fragment leaves the address.
- The route experiment (`TestIdentityPairingRoutesEndToEnd`, skipped unless its fixture runs it)
  drives the local routes against Cloud's real routes with the product's browser modules: machine
  denial keeps nothing; approval persists once; reload then sealed open; sign-out clears
  plaintext; rotation needs a new handover; revocation deletes records.

Not proven here: a real browser's IndexedDB and the visual review of both sheets; a browser that
has never taken the legacy pairing (it stays on the pairing screen, where this handover does not
run yet).

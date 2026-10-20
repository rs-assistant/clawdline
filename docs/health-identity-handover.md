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

- No route or console screen runs the identity pairing that writes `content-key-epoch-v1.json`;
  until one does, an opted-in daemon reports `key_handover_required` and sends nothing sealed.
- No console control sets `cloud_health_owner_retention`; it is settable through `clawdline setting`.
- The owner's reader of sealed attachments is not part of this change.

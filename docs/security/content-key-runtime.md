# Content-key runtime cutover (in preparation)

**Status: preparation only.** This page describes a runtime foundation for
rotating this machine's master content key after a revocation. Nothing in the
daemon constructs it yet: `Link`, pairing and the direct carrier are unchanged,
production rotation is not enabled, and revocation is not complete. Do not cite
this page as evidence that revoked devices lose access.

## The problem it closes

`Relay.PublishTracked` used to read `r.Secret` and `r.KeyID` as two fields, and
the spool's drain wrote whatever row it took. A rotation landing between those
steps could seal under one key and label with another id, or let a row sealed
under the retired key be written after the rotation reported success, because
the drain had already taken the bytes (`Spool.SendNext`, `Spool.ResendCorrelated`).

## The pieces

| Piece | Where | What it does |
|---|---|---|
| `KeyEpoch` | `internal/domain/cloud/key_epoch.go` | Holds one `ContentKeyState` (epoch, id, key) behind an `RWMutex` fence. |
| `KeyEpoch.Use(fn)` | same | Runs `fn` with the state while the fence is held shared. A halted epoch answers `ErrKeyEpochHalted` and does not run `fn`. |
| `KeyEpoch.Current()` | same | A snapshot for opening an inbound envelope; halted answers `ErrKeyEpochHalted`. |
| `KeyEpoch.Cutover(rotate, retire)` | same | Closes the fence, waits for every running `Use`, calls `rotate(old)`, calls `retire(old, next)`, installs `next`, reopens. |
| `KeyEpoch.Recover(recover, retire)` | same | Reopens a halted epoch from what the durable store says now. |
| `ContentKeyState.Sendable(keyID)` | same | `rk-*` or the installed id; `""` (unlabelled) is not. |
| `Relay.Keys` | `internal/transport/cloud/relay.go` | When set, every master-key publish runs inside `Use`, from reservation through `LabelKey`, seal, `SealCorrelated` and `MirrorStatus`. |
| `Options.KeyEpoch` | `internal/adapters/cloud/transport.go` | When set on a machine: inbound opens only with the installed key and only when the envelope names the installed id; every drain write and reconnect re-send runs under the fence and re-checks the row. |
| `SpoolRow.KeyID`, `Spool.LabelKey` | `internal/adapters/cloud/spool.go` | Which key a row is sealed under, recorded while the row is still reserved. |
| `Spool.RetireKeys(keep)`, `Spool.RetireForCutover` | same | Burns every live row (reserved, ready, sent) whose id `keep` refuses, as `content_key_retired`. |
| `Spool.SendNextKeyed(keep)` | same | `SendNext` that burns a ready row under a refused id instead of handing it out. |

With neither `Relay.Keys` nor `Options.KeyEpoch` set, every call behaves as it
did before: `Secret`/`KeyID` seal, `ContentKey` opens, the drain is unfenced.
The viewer's `ContentKeyFor`, per-connection `rk-*` terminal keys and the peer
paths never go through `KeyEpoch`.

## The cutover, in order

1. `Cutover` takes the write lock. Go's `RWMutex` stops new `Use` callers once a
   writer waits, so no new publish or send starts.
2. It waits for every `Use` already running. Socket I/O has an
   `OpeningTimeout` deadline, but acquiring the socket write mutex and finishing
   callbacks (including `MirrorStatus`) have separate owners. The complete
   cutover wait has no deadline here; integrated carriers must establish and
   measure their callback and lock-wait bounds.
3. `rotate(old)` is the durable step, normally
   `ContentKeyStore.RotateContentKey(operationID, old.Epoch, nil)`. It runs
   under the lock and must not publish.
4. `retire(old, next)` is normally `Spool.RetireForCutover`. It burns, under the
   spool's own lock, every reserved, ready and sent row not labelled with
   `next`'s id or an `rk-*` id. A sent row is marked `transport_uncertain`. Each
   burned row is handed out by `TakeBurned`, so its waiter hears
   `SettleBurned` on the next drain tick. A late relay receipt for it is
   `late_ignored`. The sequence counter does not move back.
5. `next` is installed and the fence reopens.

Lock order is always `KeyEpoch`, then the spool. A function passed to `Use` must
not call `Use`, `Current`, `Cutover` or `Recover`. A `MirrorStatus` hook runs
inside `Use` and must not publish through the relay.

## Failure behaviour

- **Any failure in `rotate`, validation or `retire` halts the epoch.** The old
  state stays installed, and `Use` and `Current` answer `ErrKeyEpochHalted` (the
  cause is wrapped). Nothing is sealed, sent, re-sent or opened under the master
  key: the store may already have advanced its floor, so carrying on under the
  old key would mean sending under a retired key.
- Every adopted state must have the exact `ms-1` legacy ID or
  `mk-<epoch>-<SHA256(key)>` ID. A higher epoch cannot reuse an old ID. A rotation
  that answers an epoch that is not later or an invalid state is refused (`ErrKeyEpochRegress` / `ErrContentKeyState`) and halts.
- A halted epoch refuses `Cutover`. The next step is `Recover`, not a second
  rotation.
- `Recover(store.RecoverContentKey, spool.RetireForCutover)` adopts what the
  store holds. If it holds the installed epoch and key, the rotation never
  became durable and the epoch simply reopens. If it holds a later epoch, the
  old rows are retired first and then the new state is installed. If it holds an
  earlier epoch, or another key at the same epoch, recovery answers
  `ErrKeyEpochRegress` and stays halted. A failed retire leaves the epoch
  halted at the old state, so the next recovery retries it.
- While halted, the drain sends nothing and keeps ticking. A reconnect re-send
  stops at the first refused row. Rows still `sent` are settled by their attempt
  window if recovery does not come in time.
- An inbound envelope whose id is not the installed one is dropped as
  `decrypt_failed`, with `key=not_current` in the log line. No old master key is
  kept for opening ordinary requests.
- With `KeyEpoch` set, an unlabelled spool row is unknown, and unknown is not
  current: the drain burns it instead of writing it. Today the relay is the only
  production producer, and it labels every row. `cmd/clawdline`'s
  `publishProbe` does not, so it must not share a spool with a fenced transport.

## Bounds

No new bound is introduced. The fence holds no queue: waiting publishers are
goroutines owned by their callers. The socket I/O deadline does not bound
callback execution or mutex acquisition, so no whole-cutover latency bound is
claimed here. Retired rows become tombstones under the existing
`cloud.spool_receipts` cap and `TombstoneRetention`.

## Obligations of the next owner

These are not done, and the foundation is not complete until they are:

- **Construct and share one `KeyEpoch`** from
  `ContentKeyStore.LoadOrCreateContentKey`, and set it on both `Relay.Keys` and
  the machine transport's `Options.KeyEpoch`. Setting only one is not
  supported: a fenced transport would burn the unfenced relay's rows as unknown
  or stale.
- **Wire `Link.Revoke` (or its successor) to `Cutover`** once the Cloud scope
  carrier lands, with a stable operation ID per revocation so that a retried
  rotation resumes rather than mints again. On a halted epoch, run `Recover`
  before anything else, including at daemon start.
- **Distribute the new key to remaining viewers** through the separate rekey
  control frame owned by the crypto work (`domain/rekey.go`). This page adds no
  protocol for it.
- **Retire old frames the direct carrier queued itself.** `MirrorStatus`
  returning means the carrier accepted the bytes, not that they were delivered.
  A carrier that buffers an old-key frame must drop it at cutover; nothing here
  does that.
- **Viewer side.** `ContentKeyFor` still answers by `(sender, keyID)`. Accepting
  `mk-*` ids from a paired machine is the viewer's own change.
- Acceptance as a production rotation, including a real revocation run, has not
  been done.

## Independent review follow-up

The independent static review of `3fadf9a24` retained no confirmed exploitable
finding in the inactive foundation. Its trusted-callback ID concern is hardened
by validating the complete epoch-derived key ID on construction, cutover and
recovery. A higher epoch returning its previous ID was accepted before this
validation; the regression test now requires rejection and a halted epoch.
The whole-wait deadline claim has been corrected above. Inbound decryption still
takes a snapshot before dispatch: integration must establish a fresh active-pin
authorization point for in-flight requests. This outbound fence alone does not
prove immediate cancellation of all in-flight inbound work.


The realistic epoch fixtures exposed a compatibility gap: the full epoch/hash ID
is longer than the previous 64-byte envelope key-ID limit. The daemon's parser
now permits 128 bytes; this is an additive reader change, not a new ID format.
Browser and private relay readers must accept the same bound before production
rotation is enabled. The existing short IDs remain valid. The relay reader
upgrade and actual end-to-end acceptance remain pending.

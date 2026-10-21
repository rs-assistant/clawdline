# Browser content-key refresh (disabled by default)

The browser implementation composes the existing [v1 control contract](content-key-rekey.md)
with the paired machine's durable state. It is preparation for the relay and daemon runtime;
this change does not enable production rotation or establish future-content revocation.

## Authority and storage

`CloudViewerSession.connect` passes the authenticated account/device, persistent non-extractable
Ed25519 signing key and corresponding public bytes. `machinePairing` supplies a directly paired
machine's pinned, non-extractable verification key. Account roster membership, an envelope's
identities, and `allowWrites` do not grant rekey authority. Legacy bindings remain on their
existing path and must use explicit pairing to become eligible. No persistent private key is
exported. The cryptographic helper accepts a verification `CryptoKey` in addition to raw public
bytes; canonical messages, signatures, domains and KDF bytes are unchanged.

`ViewerRekey.request` creates a fresh extractable X25519 key pair, random operation ID and
32-byte random nonce. Epoch 1 is initialized only for `ms-1`; other existing bindings must
carry an authenticated durable floor. A new explicit pairing records the epoch of its
authenticated handover key ID. The fresh ephemeral is extractable solely for the primitive's
public-key match. It is never persisted or sent as a private key.

`ViewerRekey.answer` reserves the one pending operation before its first asynchronous step.
The crypto helper verifies the exact signed offer, account/machine/viewer, hash, machine
signature, expiry, recipient and floor, then decrypts and verifies the epoch/key ID. The
IndexedDB `adoptMachineRekey` transaction compares the latest stored floor, key ID, pin
generation and machine identity; it writes the master key, key ID, epoch and last consumed
operation together. Only its completion may change client memory. Aborts change neither
durable state nor memory. Competing tabs cannot replace a higher epoch or a different key
at the same epoch. Explicit pairing and legacy migration also preserve existing floors.
The original account-level compatibility key is never replaced by refresh.

The pending operation is local and reserved while adoption is in progress. Duplicates cannot
start a second adoption, the consumed operation is recorded in the binding, and restart or
disconnect loses the ephemeral and pending offer. An unsolicited answer after restart is
rejected even if its signature is valid. A transaction's authority check is synchronous inside
its readwrite request callback; a disconnect after that check may leave a valid durable commit
for the next boot, but cannot resume the disconnected client or adopt into its memory.

## Capacity and recovery

Each client holds one operation across all machines (`VIEWER_REKEY_PENDING_LIMIT = 1`), with
the contract's 120-second lifetime and a 120-second minimum offer interval
(`VIEWER_REKEY_COOLDOWN_MS`). It retains zero unknown envelopes and schedules zero retries.
New traffic after the interval may request another operation; expiry itself sends nothing.
Both browser-owned bounds are registered with English and Taiwan Traditional Chinese copy,
and daemon readings explicitly report that live browser usage cannot be measured.

The unknown-key receive path does not advance replay state. It only waits for local offer
construction, never for the network answer: raw `content_key_answer` travels through the
same `messageChain` without deadlock. Successful adoption re-subscribes the machine's held
channels, including terminal subscriptions, to obtain retained state. Idempotent outstanding
reads reissue their original requests under the adopted key; actions are never replayed.
Reads queued during refresh keep their existing deadline and waiter rather than adding a
second queue. A read-only client can rekey and subscribe without publishing a command.
Its existing passive Session-refresh deadline remains authoritative if the relay has no
retained state; a discarded envelope is never reported as delivered. There is no claim that
subscription alone forces a disconnected machine to publish new data.

## Enable switch and deployment order

`CloudViewerSession` and `CloudClient` accept `enableViewerRekey: true`; both default to false.
Production boot does not set it. The client additionally requires the trusted public device key
and `persistMachineRekey` callback. Tests opt in explicitly with a synthetic relay and database.

1. Integrate the native/crypto/epoch source branch and the root's Go envelope key ID compatibility
   change. The existing durable `mk-<epoch>-<full SHA256 hex>` format stays intact.
2. Deploy additive raw offer/answer validation and routing in the private relay. Its
   `DEFAULT_LIMITS.maxKeyIdLength` must accept 128 bytes before any producer emits these IDs.
   The browser's named `MAX_CONTENT_KEY_ID_BYTES` and Go `MaxKeyIDBytes` must both be 128.
3. Deploy the hosted viewer with the switch off. Integrate daemon active-pin authorization,
   rotation/publication barriers, answer handling and fresh snapshot recovery.
4. Accept both ends with read-only B, revoked A, cross-tab races, persisted floors, restart,
   dropped/duplicated frames and recovery. Only then opt production boot in and enable the
   producer. Rollbacks must retain durable floors.

## Verification

`viewer-rekey.test.ts` uses real WebCrypto and the actual client/message chain, with only relay
transport and storage faked. `viewer-rekey-storage.test.ts` models serialized IndexedDB
transactions with isolated writes and abort rollback. Tests cover read-only adoption,
signature/context/recipient failures, exact pending operations, expiry, duplicate and unsolicited
answers, aborts, competing floors, pairing generations, legacy migration, account alias preservation,
read resumption, subscription recovery, bounds and full-length durable IDs. The fake database
does not establish real browser crash consistency or production relay support. Root owns final
integration, browser/daemon acceptance, deployment and enabling production.

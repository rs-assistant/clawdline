# Connection health: UI integration handoff

Status: 2026-10-20. The six screens (M01–M06) and the agent evidence view exist as React modules
under `web/console/src/cloud/admin/health/`. They are **not wired** into the admin shell, routes,
catalogs or any backend: nothing on `/admin` shows them yet, and no production request is made.
This page is what the root needs to connect them. The design is
[admin-connection-health-design.md](admin-connection-health-design.md); the product plan is
[admin-connection-health.md](admin-connection-health.md).

## Files

| File | What it is |
| --- | --- |
| `types.ts` | The consumer contract `clawdline.connection-health.v0-draft`: `HealthSnapshot`, `HealthQuery`, and the closed vocabularies (`OUTCOMES`, `QUALITIES`, `SCOPES`, `SEGMENTS`, `STAGES`, …). |
| `parse.ts` | `parseHealthSnapshot(unknown)` — field-for-field validation; throws `HealthContractError` (`contract_schema`, `contract_shape`, `contract_bound`). `HEALTH_BOUNDS` lists the row bounds. |
| `model.ts` | Pure view decisions: `worstOutcome`, `latencyView`, `rateView`, `placeLane`, `placeInstant`, `stageProgress`, `stageOffsets`, `logWidth`, `rankIncidents`, `keepSelection`, `healthPath`/`healthTarget`, `buildEvidence`. |
| `source.ts` | The data boundary: `HealthSource`, `loadHealth` (deadline + cancellation + typed failure), `httpHealthSource(address, fetch)`, `failureForStatus`, `healthReducer`. |
| `useHealth.ts` | `useHealth(source, query, live)` — one request outstanding, 15 s refresh while live, cancelled on query change/unmount, scope change starts empty. |
| `words.ts` | English and Taiwan Traditional Chinese tables (`healthWords(locale)`), plus `healthDuration`, `healthMs`, `healthClock`. |
| `parts.tsx` | Shared pieces: `OutcomeBadge`, `QualityChip`, `Panel`, `HTable` (cards below 560 px), `Timeline` (chart ⇄ table), `NavLink`, `Crumb`; the `HealthNav` and `ScreenProps` types. |
| `screens.tsx` | `M01Overview`, `M02Machines`, `M03Services`, `M04Trace`, `M05Incident`, `M06Monitoring`, `EvidenceView`. |
| `ConnectionHealth.tsx` | The page: scope/window/live controls, loading/failed/stale states, routes to a screen. |
| `health.css` | Scoped `.adh-*` styles on the existing admin tokens; one new admin-local token, `--lane-ok`. |
| `health.test.ts` | Module tests (picked up by `tools/console-tests.mjs` automatically, as every `*.test.ts` under `web/console/src` is). |

## Integration API

```tsx
import { ConnectionHealth } from "./health/ConnectionHealth"
import { healthTarget, healthPath } from "./health/model"
import { httpHealthSource } from "./health/source"

<ConnectionHealth
  source={source}                 // HealthSource; see below
  target={healthTarget(pathname)!}// HealthTarget; null means "not a health address"
  nav={{ href: healthPath, go: (t) => navigate(healthPath(t)) }}
  locale={adminLocale}            // "en" | "zh-TW"
  scopes={["cloud_operator"]}     // each scope the person holds, authorized separately
/>
```

- **`scopes`** must come from the server's authorization decisions. A strict superuser gets
  `cloud_operator`; a paired owner gets `paired_owner` only from the paired-owner authorization.
  One never implies the other, and an empty list renders a refusal without issuing a request.
- **`source`** is the only way data arrives. There is deliberately no default address:
  `httpHealthSource((q) => url, fetch)` needs the root to name the route once the backend contract
  lands. It sends the page's own session cookie (`credentials: "include"`) and no other credential.
- **`nav`** keeps every navigation a real link (`href`) with in-place routing on a plain click.

### Proposed addresses (not implemented)

`healthPath`/`healthTarget` use `/admin/health`, `/admin/health/machines[/<id>]`,
`/admin/health/services`, `/admin/health/trace/<id>`, `/admin/health/incident/<id>`,
`/admin/health/monitoring`, `/admin/health/evidence/<id>`. Ids are opaque and URL-encoded. Window
and scope are page state; restoring them from the URL is left to the root (below).

### Proposed backend reads (not implemented)

The product plan proposes Cloud operator reads under `/su/v1/health/` and separate tenant-bound
owner reads. This UI expects one snapshot per `HealthQuery { scope, window_s, until }`; if the
backend splits it per screen, wrap the calls in one `HealthSource.load` that assembles the
snapshot, or reconcile `types.ts` with the landed schema. The snapshot shape is a draft for that
reconciliation, not a frozen contract.

## Truth rules the code enforces

- A missing number is refused at parse time, never defaulted to zero. Empty latency renders
  "Unknown"; fewer than 20 samples replaces p99 with "too few samples (n)".
- Zero requests is `no_requests`, never a 0 % error rate or 100 % success.
- Timeline time without an observation is drawn as unknown hatching (`placeLane`), never healthy.
- A forbidden item that still carries text is refused (`contract_shape`): the source must filter
  it. The page shows only a count of hidden items; the export keeps `{source, forbidden: true}`.
- A trace stage has a time only with a receipt. `stageProgress` gives `last_proven_stage`,
  `active_stage` (only from an open span), `next_unobserved_stage`, `visibility_limit_at`.
- "Relay socket accepted" and "UI applied (not an acknowledgement)" are separate stages; M03 states
  that a socket write proves only that bytes left Cloud.
- Suspected causes carry their basis (source, ordering only, inference) and sit apart from facts and
  missing evidence. Certainty and recovery-verification are separate badges.
- Offline comes only from presence expiry; no cause is inferred.
- A failed refresh keeps the last snapshot with a stale notice; a first-load failure shows no
  figures and the typed reason. A superseded or unmounted load is dropped.
- A chosen incident survives live refreshes; if new data lacks it, a notice says so.
- The export (`buildEvidence`) lists fields one by one — a field the source adds later never
  reaches it — and is capped at 512 KiB. `example: true` snapshots show a sticky banner on every
  view and carry `example: true` in the export.

## What the root still has to do

1. **Route and shell.** Add the health group (M01, M02, M03, M06) to `ADMIN_ROUTES`/the sidebar in
   `AdminGate.tsx` and `words.ts`, render `<ConnectionHealth>` inside the admin main area, and keep
   it independent of machine CloudGate as the rest of admin is.
2. **Backend source.** Name the real route(s) in `httpHealthSource`, reconcile `types.ts` with the
   landed backend schema, and keep a refusal (401/403) a typed refusal with no data flash.
3. **Catalog.** Move `words.ts` tables into the public catalogs (`check-source-catalog.mjs` reads
   named tables; this one is not registered yet) or register it there; the strings ship in both
   languages already.
4. **Capacity.** Register `HEALTH_BOUNDS`, `EVIDENCE_MAX_BYTES`, `HEALTH_DEADLINE_MS` and
   `HEALTH_REFRESH_MS` with the capacity contract (`internal/domain/capacity`) or replace them
   with values the backend publishes.
5. **URL state.** Restore window, scope and paused instant from opaque URL parameters if wanted.
6. **Browser acceptance.** Render with a real source at desktop and 360 px, keyboard-only
   (find incident → open → trace → export), screen-reader labels on lanes, and focus after route
   changes. None of this was done by the implementing task.

## Not verified

- No browser rendered these screens; there is no route to them yet. Layout, focus order, contrast
  in place and phone width were reasoned from the existing admin CSS, not observed.
- The module tests exercise parsing, view decisions, the source boundary, the reducer and the copy
  tables, not React rendering (the console has no DOM test environment).
- Every bound, cadence and threshold above is the plan's proposal, not a measurement.

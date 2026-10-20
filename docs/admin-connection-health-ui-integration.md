# Connection health: UI integration

Status: 2026-10-20. `/admin/health` is a page of the hosted admin shell. It reads the Cloud's
stored operator evidence (`GET {api}/su/v1/health/…`, private contract
`docs/connection-health-contract.md` in the Cloud repository, §3, §3a and §10) through an explicit
adapter, and draws M01–M06 and the evidence view from what those reads return. It is **not** the
complete monitor of [admin-connection-health.md](admin-connection-health.md): the Cloud has no
presence contract, no durable incidents, no viewer producer and no owner-sealed diagnostics yet,
and every screen says so where it would otherwise have shown them. The Cloud API that serves these
reads (tasks 2a4ecc78 and 8de84319) is committed, not deployed; nothing here has called production.
The design is [admin-connection-health-design.md](admin-connection-health-design.md).

## Addresses

All under the existing admin shell (`AdminGate.tsx`), beside the business pages, signed in with the
same revocable cookie session and independent of the machine `CloudGate`. A failed business
overview does not hide this page; a refused health read does not break the business pages.

| Address | Screen | Reads (at most two in flight) |
| --- | --- | --- |
| `/admin/health` | M01 overview | `overview`, `outcomes`, `incidents` |
| `/admin/health/machines` | M02 machines | `sources`, `outcomes` |
| `/admin/health/machines/<machine_id>` | M02 one machine | `sources?machine_id`, `outcomes?machine_id`, `events?machine_id` |
| `/admin/health/services` | M03 Cloud services | `overview`, `sources`, `events` |
| `/admin/health/trace/<correlation>` | M04 trace | `trace/<correlation>`, `outcomes` |
| `/admin/health/incidents[/<id>]` | M05 incidents | `incidents` or `incidents/<id>` (501), `outcomes` |
| `/admin/health/monitoring` | M06 monitoring | `overview`, `sources`, `outcomes` |
| `/admin/health/evidence[/<correlation>]` | evidence record | `outcomes`, `events` or `trace/<correlation>`, `sources` |

Query: `window` (900, 3600, 21600 or 86400 s; default 3600), `until` (a past instant: paused), and
`account` (an operator filter, never authority). A malformed value falls back to live/default; ids
must match the Cloud's opaque-id rule. Refresh, a shared link, Back and Forward land on the same
screen and scope (`health/route.ts`). A screen change moves focus to the page heading; a refresh
or a scope change does not.

## Files

| File | What it is |
| --- | --- |
| `health/stored.ts` | The adapter: v1 envelope, overview, events (record/1 and record/2), sources, outcomes and incidents, read field by field. Refuses `schema_version ≠ 1`, an overall `outcome` other than `unknown`, and any list past the Cloud's own bound. `stageRole` names service stages that wait on downstream. |
| `health/cloud.ts` | The loader: `planFor` (fixed reads per screen), `runReads` (concurrency 2, per-read deadline, overall budget, typed failures, refusal cancels everything), `storedReducer` (stale-per-read, refusal clears), cursor paging with a frozen window. `HEALTH_UI_LIMITS`. |
| `health/route.ts` | Addresses and scope. |
| `health/stored-screens.tsx` | The screens. |
| `health/stored-export.ts` | The evidence record: listed fields only, ≤ 512 KiB, `not_an_incident_bundle: true`. |
| `health/useStored.ts` | One load at a time; 15 s live refresh; cancelled on route or scope change. |
| `health/ConnectionHealth.tsx` | Page controls and whole-page states. Loaded lazily by `AdminGate.tsx`. |
| `health/fixture.ts` | Development-only example answers (see below). |
| `health/stored.test.ts` | Module tests (mocked fetch; run by `npm test`). |
| `health/{types,parse,model,source,useHealth,screens,parts}.ts(x)` | The earlier draft snapshot design (`v0-draft`). `parts.tsx` is shared; the rest is the full-product rendering target and is not reachable from a route until producers and the Cloud can fill it. |

Copy is in `health/words.ts` (English and Taiwan Traditional Chinese); the page reads it from the
public catalogs as `health.<key>` (`tools/check-source-catalog.mjs` keeps them in step). Bounds are
registered as `console.health_ui_*` (`internal/domain/capacity/register_connection_health_ui.go`).

## Truth rules the code enforces

- The overall verdict is always "unknown — no presence contract". Evidence age (`fresh`/`stale`) is
  shown as evidence age and never as online or offline.
- `null` is unknown, never zero; a count past its cap is shown as "at least n"; a missing per-event
  split is "unknown", not zeros. No group is not success.
- Group states are the Cloud's `cloud-derive/1` words with their `group_quality`/`group_reason`;
  no incident, root cause or locked function is derived from them.
- A read that fails says why in its own panel; the reads that answered stay on screen. A failed
  refresh keeps that read's last answer marked stale with its time. A first load where every read
  failed shows no figures and each reason.
- 401, and 404 `not_found` from the operator surface, end the load, abort the other reads and clear
  every figure before the shell signs in again or says access is refused. There is no owner scope
  on this page: superuser does not grant it, and no owner read is made.
- Producer wall clocks are shown as "display only" and never subtracted; no latency is computed.
  Service `elapsed_ms` is shown as the service's own per-stage time, with stages that wait on
  downstream (`account_dispatch_started`, `storage_started`, `api_dependency_started`) labelled so,
  and `fanout_sent` labelled "handed to socket only". `build_commit` is labelled producer-declared.
- Incidents, incident export, owner diagnostics, relay/viewer account-scoped stages, end-to-end
  latency, queues and probes are labelled unsupported with the reason.
- The stage note now reads: a completed socket write means only that the local socket
  implementation accepted the bytes; it does not prove delivery or viewer receipt.

## Bounds (`HEALTH_UI_LIMITS`, registered)

Per read 6 s; per load 12 s; 2 reads in flight (the Cloud's per-principal admission); 3 reads per
load; 1.5 MiB per body (refused unread above); 200 rows per page; 5 pages held (older paging only
while paused, because the Cloud's cursor hashes the window); 512 KiB evidence record; 64-character
ids. These are this page's choices against the Cloud's documented bounds, not measurements of
production.

## Development harness

A development build only, with an explicit flag; a production build drops the branch and the module
(checked: `grep fixture_mac_a` over a production build with the flag set finds nothing).

```sh
cd web/console
VITE_HOSTED_CONSOLE='{"v":1,"app_origin":"http://127.0.0.1:5274","api_origin":"http://127.0.0.1:5274","relay_url":"ws://127.0.0.1:5274/v1/connect"}' \
VITE_ADMIN_HEALTH_FIXTURE=normal npx vite --host 127.0.0.1 --port 5274 --strictPort
# open http://127.0.0.1:5274/admin/health
```

Scenarios: `normal`, `partial`, `errors` (outcomes 503, events 429), `empty`, `slow` (every read
times out), `signed_out`, `forbidden`, `service`. The harness skips the session, answers in memory
(no request leaves the browser), and shows a fixture banner on every view; the evidence record
carries `fixture: true`. The other admin pages say they need a real session.

## Bundle

Production build, hosted declaration: `AdminGate` JS 112.53 kB → 117.32 kB (gzip 33.26 → 35.09);
the lazily loaded `ConnectionHealth` chunk is 90.50 kB (gzip 28.27) JS and 9.80 kB CSS, fetched
only on `/admin/health`.

## Not verified, and what the root still owes

- **No browser rendered these screens in this task.** Owed: desktop and 390 px, light and dark,
  keyboard-only pass (open M01 → a group → its trace → evidence → copy), screen-reader names,
  focus after route changes, for each fixture scenario, then against a deployed Cloud.
- No production read: the Cloud reads are not deployed. Field names follow the committed Cloud
  branches (2a4ecc78 `4a40b35`, 8de84319 `1414a55`); a later Cloud change needs the adapter updated.
- Service evidence on the newest events page only: there is no `source` filter on `events`, so M03
  shows Relay/API records among the page it holds.
- Not built anywhere yet: durable incidents and bundles, owner encryption, presence contract,
  viewer producer, external probe, account-attributed service evidence, daemon uploader.

# Connection health: UI integration

Status: 2026-10-20. `/admin/health` is a page of the hosted admin shell. It reads the Cloud's
stored operator evidence (`GET {api}/su/v1/health/…`, private contract
`docs/connection-health-contract.md` in the Cloud repository, §3, §3a, §10, §11 and §12) through an
explicit adapter, and draws M01–M06 and the evidence view from what those reads return. It is
**not** the complete monitor of [admin-connection-health.md](admin-connection-health.md): the Cloud
has no presence contract, no viewer producer, no probes and no owner-sealed diagnostics yet, and
every screen says so where it would otherwise have shown them. Durable incidents, their immutable
revisions, the audited export and the operator acknowledgement (Cloud commit `fac588c`) are
read and drawn. The Cloud API that serves these reads is committed, not deployed; nothing here has
called production.
The design is [admin-connection-health-design.md](admin-connection-health-design.md).

## Addresses

All under the existing admin shell (`AdminGate.tsx`), beside the business pages, signed in with the
same revocable cookie session and independent of the machine `CloudGate`. A failed business
overview does not hide this page; a refused health read does not break the business pages.

| Address | Screen | Reads (at most two in flight) |
| --- | --- | --- |
| `/admin/health` | M01 overview | `overview`, `outcomes`, `incidents` (open ones link to M05) |
| `/admin/health/machines` | M02 machines | `sources`, `outcomes` |
| `/admin/health/machines/<machine_id>` | M02 one machine | `sources?machine_id`, `outcomes?machine_id`, `events?machine_id` |
| `/admin/health/services[?source=…&source_id=hs_…]` | M03 Cloud services | `overview`, `sources`, `events?source&source_id` |
| `/admin/health/trace/<correlation>` | M04 trace | `trace/<correlation>`, `outcomes`, `incidents` |
| `/admin/health/incidents` | M05 incident list | `incidents` (7 days by `opened_at`, 50 rows), `outcomes` |
| `/admin/health/incidents/<hi_…>[?revision=n]` | M05 one revision | `incidents/<id>?revision=n`; export and acknowledge on demand |
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
| `health/stored.ts` | The adapter: v1 envelope, overview, events (record/1 and record/2, with `state` only on `state_after_loss`), sources, outcomes, the incident list, one incident revision and its bundle, read field by field. Refuses `schema_version ≠ 1`, an overall `outcome` other than `unknown`, any list past the Cloud's own bound, a `root_cause` other than `not_derived`, any hypothesis, and a revision chain that is not 1..n. `stageRole` names service stages that wait on downstream. |
| `health/incident-export.ts` | The audited export: read under 1 MiB (reading stops past it), checked against the incident and revision asked for, its bundle re-hashed with WebCrypto over the Cloud's key-sorted JSON; and the acknowledgement POST. |
| `health/incident-screens.tsx` | M05 list and revision view, the export panel and the two-step acknowledgement. |
| `health/cloud.ts` | The loader: `planFor` (fixed reads per screen), `runReads` (concurrency 2, per-read deadline, overall budget, typed failures, refusal cancels everything), `storedReducer` (stale-per-read, refusal clears), cursor paging with a frozen window. `HEALTH_UI_LIMITS`. |
| `health/route.ts` | Addresses and scope. |
| `health/stored-screens.tsx` | The screens. |
| `health/timeline.ts` | Pure mapping from stored reads to charts: the window axis and its bands, per-boot lanes of receipt marks, sequence coverage, source freshness, service source × stage, one operation's ordered stages, the weakest-quality summary and the problem list. Tested by `health/timeline.test.ts`. |
| `health/timeline-view.tsx` | The charts that draw those shapes, each with a chart/table switch, the problem summary and quality strip every screen opens with, and one "requested, not collected yet" panel per screen. |
| `health/stored-export.ts` | The evidence record: listed fields only, ≤ 512 KiB, `not_an_incident_bundle: true`, `incident_bundles: not_included_use_incident_export`. |
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
- Owner diagnostics, relay/viewer account-scoped stages, end-to-end latency, queues, minute
  aggregates and probes are labelled unsupported with the reason.

## Incidents (M05, and links from M01 and M04)

- The list is the Cloud's, by `opened_at` over the 7 days ending where the page's window ends. An
  expired row shows its id and expiry and no state; `facts: null` is unknown. With incidents
  turned off (`source_coverage.incidents: disabled`) the page says none are being opened, never
  "no incidents". Refusal counters are shown, because an absent incident may be a refused one.
- A revision view separates **what the records prove** (frozen facts, last proven stage, the
  stage still active), **what is missing** (sequence gaps, producer loss counts, facts refused by
  the bound, owner-private fields never collected), **cause** (always "not derived", no
  hypotheses) and **context**. Recovery evidence and human review are two panels: `closed` reads
  "closed by review" with its closure, never healthy, and states never move with time.
- Hash, size, written time, retention of the bundle and of the raw rows, the previous revision
  with its hash, and late evidence are shown. Revisions are links (`?revision=n`, Back/Forward
  restore them); an older one says it is not the latest. `410 incident_expired` shows its
  expiry and nothing from before; `404 revision_not_found` offers the latest.
- **Export** fetches `…/incidents/<id>/export?revision=n`, reads at most 1 MiB, refuses any key
  beyond `schema_version`/`export`/`bundle`, checks the incident id and revision, parses the
  bundle, and recomputes `content_hash` as sha256 over the key-sorted JSON of `bundle` with
  WebCrypto. Only then are the exact received bytes offered for download (`<id>.r<n>.json`) or
  copy. `503 audit_unavailable`, a mismatch or an oversized body keeps nothing. The bytes live in
  the panel of that incident, revision and account; changing any of them unmounts it.
- **Acknowledge** is a button, then a confirmation that says what revision it writes and that it
  does not mean recovery; one `POST …/acknowledge` with the page's cookie, the browser supplying
  the console's `Origin` (`403 bad_origin` is shown as such). No read ever acknowledges.
- 401 and the operator surface's 404 from an export or acknowledgement clear the page as a read's
  would. Owner diagnostics stay unsupported; nothing here grants owner scope.
- M01's incident tile and panel list the open incidents on the first page (active, recovering,
  awaiting review), linking to each. M04 lists the incidents whose group names its correlation
  (first page only, said so) and the producer-declared builds per source and boot in its records.
- The stage note now reads: a completed socket write means only that the local socket
  implementation accepted the bytes; it does not prove delivery or viewer receipt.

## What the charts draw, and what they refuse to

Every screen opens with **What to look at first** (loss, stall, slow and released groups, stale
sources, missing sequence records and failed reads, worst first, with the newest one's machine,
stage and evidence age) and the **weakest evidence** strip: the overall quality is the weakest of
the reads the screen relies on, a failed or stale read or a partial group caps it at partial, and
each read's own quality is listed. It never says more than "only what these reads returned".

- **Time lanes** (M01/M02/M04/M05 groups, M02 machine and M04 records, M03 service records): one
  lane per source and boot, ordered by the Cloud's boot generation (unknown last). Each record or
  group is a glyph at its server receive time. The track between marks is drawn "not observed",
  never healthy. Records outside the query window are counted at the edge ("n earlier/later"), not
  drawn. Bands mark time before the retention floor, time after the read's `as_of`, rows older
  than the oldest held row of a list with more pages ("not loaded"), and listed sequence gaps
  placed between the receipts of their nearest held neighbours (otherwise counted "cannot be
  placed in time"). A record received after a higher sequence of the same boot is marked late;
  a producer monotonic or wall clock that goes backwards against its own sequence is counted per
  boot. Producer clocks are never compared with receive time or across hosts. Clock uncertainty
  is shown, or "unknown".
- **Sources** (M02, M06): each source's newest receipt on the window (fresh or stale glyph, age,
  declared cadence) and each boot from its first to its last receipt as a dotted bracket that
  says not every moment between was observed.
- **Sequence coverage** (M01, M06): received ranges and listed gaps in producer-sequence space,
  per source and boot, with retention floor and drop counters beside it.
- **Service stages** (M03): record/2 rows on the loaded page as source × stage in contract order,
  a bar for each stage's own largest `elapsed_ms` (downstream waits striped, socket handoff
  dotted; never summed, never end-to-end), outcome counts, and one bottleneck: a still-pending
  stage, else a failed one, else the largest own time, with its evidence age from `as_of`. The
  source and source-id selectors are the Cloud's own `source`/`source_id` filter: a change is a
  new address, so held pages are dropped and paging restarts under a cursor bound to that filter.
  The line under them says whether every record of the filtered window is loaded or how many of
  how many pages are held.
- **One operation** (M04): stored records per source and boot in producer order, with sequence
  holes as steps and late arrivals marked. Separate sources are not merged into one order and no
  waterfall or end-to-end time is drawn.
- Unsupported parts (latency, presence, relay/viewer stages, spans, probes) share one
  "requested, not collected yet" panel per screen with their exact limitation.

## Bounds (`HEALTH_UI_LIMITS`, registered)

Per read 6 s; per load 12 s; 2 reads in flight (the Cloud's per-principal admission); 3 reads per
load; 1.5 MiB per body (refused unread above); 200 rows per page; 5 pages held (older paging only
while paused, because the Cloud's cursor hashes the window); 512 KiB evidence record; 64-character
ids; 50 incidents per list over 7 days; 1 MiB incident export. Revisions (16), facts (200) and
context (20) per revision are the Cloud's bounds, refused past them as `contract_bound`. These are this page's choices against the Cloud's documented bounds, not measurements of
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
times out), `signed_out`, `forbidden`, `service`, `incidents_disabled`, `audit_fail` (every export
503 `audit_unavailable`), `tampered` (an export whose bundle no longer matches its hash). `normal`
holds one incident per state with real canonical hashes: `…a01` active (relay), `…a02` loss
(active), `…a06` recovering (failed stage), `…a03` recovery observed awaiting review (two
revisions), `…a04` closed by review without observed recovery (three revisions; r2 recovering),
`…a05` expired (410). Ids are `hi_f1000000000000000000000000000a0N`. Acknowledging in the harness
writes a closing revision for that tab only. The events read honours `source`/`source_id` and
refuses a cursor from another filter. The harness skips the session, answers in memory
(no request leaves the browser), and shows a fixture banner on every view; the evidence record
carries `fixture: true`. The other admin pages say they need a real session.

## Bundle

Production build, hosted declaration: `AdminGate` JS 112.53 kB → 117.32 kB (gzip 33.26 → 35.09);
the lazily loaded `ConnectionHealth` chunk is 90.50 kB (gzip 28.27) JS and 9.80 kB CSS, fetched
only on `/admin/health`.

## Not verified, and what the root still owes

- **The charts were not rendered in a browser by the task that drew them.** Owed by
  the root: `VITE_ADMIN_HEALTH_FIXTURE=normal|partial|errors|empty|service` (command above), each
  of `/admin/health`, `/machines`, `/machines/fixture_mac_b` (two boots, a late record, clocks
  stepping back), `/services` (stage bars, bottleneck, source selector), `/trace/fixture-trace-1`,
  `/monitoring`, at desktop and 390 px; Tab through the chart/table switches and the source
  selector. The admin shell is dark-only (`legacy/tokens.css`), so there is no light theme to check.
- **No browser rendered these screens in this task.** Owed: desktop and 390 px, light and dark,
  keyboard-only pass (open M01 → a group → its trace → evidence → copy), screen-reader names,
  focus after route changes, for each fixture scenario, then against a deployed Cloud.
- **The incident screens were not rendered in a browser by the task that built them.** Owed by
  the root, with `VITE_ADMIN_HEALTH_FIXTURE=normal` unless named: `/admin/health` (incident tile
  and open-incident panel), `/admin/health/incidents`, each of
  `/admin/health/incidents/hi_f1000000000000000000000000000a0N` for N = 1, 2, 3, 4, 6, and
  `…a04?revision=1`/`?revision=2` with Back/Forward between them, `…a05` (expired 410),
  `…a03?revision=9` (missing revision), export then download/copy on `…a03`, acknowledge on
  `…a01` (confirm, then the closing revision appears), `audit_fail` and `tampered` exports on
  `…a03`, `incidents_disabled` on `/admin/health/incidents` and `/admin/health`,
  `/admin/health/trace/fixture-trace-1` (incident link and builds),
  `/admin/health/services?source=relay&until=<a past instant>` and `…?source=daemon&source_id=hs_f100000000000000000000000000000a&until=…`
  (load older until "all records … loaded"), `signed_out`/`forbidden` on an incident. Desktop and
  390 px; keyboard only through the revision links, export, download, copy, acknowledge and cancel.
- No production read: the Cloud reads are not deployed. Field names follow the committed Cloud
  branches (2a4ecc78 `4a40b35`, 8de84319 `1414a55`, 35ba675a `fac588c`); a later Cloud change needs
  the adapter updated.
- The incident list shows its first page (50); a later page is named, not loaded. M04's incident
  links come from that page only.
- The fixture's acknowledgement is per tab and in memory; the real route's `Origin` check was not
  exercised against a Cloud.
- Not built anywhere yet: owner encryption and owner-sealed attachments, presence contract,
  viewer producer, external probe, minute aggregates, clock uncertainty, account-attributed service
  evidence.

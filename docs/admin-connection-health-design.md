# Connection health: panel design

Status: design proposal with a standalone prototype
([`admin-connection-health-preview.html`](admin-connection-health-preview.html)). Nothing here is
shipped console behaviour. Every threshold below is a **proposal, not a measurement**; every number
in the prototype is an illustrative fixture.

## 1. What the page is for

An operator, or an account owner looking at their own machine, opens the page because something
feels slow or stuck. Within one screen they must see:

1. **Whether requests are blocked, slow or failing right now**, and since when.
2. **Where on the path** (browser, relay, machine receive / process / send, collector).
3. **How much of that we can actually see** — evidence quality, per source, with its age.

Then they go overview → incident → request trace → agent evidence, ending with a structured record
a future agent can use to find the code. The page is read-only: no restart, retry, repair or
setting change appears anywhere on it.

It is not a log viewer and not a wall of green uptime tiles. Healthy is quiet grey; only a block,
slowness, an error, an offline machine or an unknown stretch draws the eye.

## 2. What was inspected

| Piece | Where | What this design takes from it |
|---|---|---|
| Admin shell: sidebar on desktop, drawer on phone | `web/console/src/cloud/admin/admin.css:82-184`, `AdminGate.tsx:303`, `:379-403` | The page is a fifth admin route in the same shell. |
| Palette and its reason for being dark only | `web/console/src/legacy/tokens.css:1-29` | Same tokens; no light theme (see §11). |
| Text-contrast rule | `admin.css:1-5` | `--faint` and `--bad` never colour words, only bars and outlines. |
| Tiles, panels, tables, phone card tables | `admin.css:343-413`, `:480-496`; `AdminDashboard.tsx:312` (`Tile`) | Tiles row, panels, table → card-per-row below 560 px. |
| Stale and demo treatments | `AdminGate.tsx:197-211`, `admin.css:141-149`, `:437-441` | A refresh failure keeps dated figures; demo is labelled and inert. |
| Unknown is a word, not a zero | `AdminDashboard.tsx:296-299`, `admin.css:424` | Empty latency is "Unknown", never 0 ms. |
| Browser-only direct history | `web/console/src/cloud/direct-history.ts:1-16`, `:100-108` | A browser's record speaks for that browser only; idle hours are not failures. |
| Machine road and offline wording | `web/console/src/pages/devices/AccountMachines.tsx:67-85` | Offline / relay / direct / failing roads; owner sheet is the owner boundary. |
| Machine Cloud status snapshot | `internal/adapters/cloud/status.go:28-55`, `:95` | Counters and drop reasons the machine already keeps. |
| Relay inbound queue bound (64 rows, refuse) | `internal/domain/capacity/register_cloud.go:170-180`, `internal/transport/cloud/relay.go:123` | The queue tile uses the registered limit, not a new one. |
| Staged truth vocabulary | `docs/cloud-request-architecture-v2.md:95-96`, `:469-500`; `docs/cloud-error-transparency.md:100-116` | Waterfall stages are separate receipts. |
| Bounded diagnostics with completeness | `docs/diagnostics.md:81-100`, `:184-212` | Evidence states what was dropped and how complete it is. |

The root's integration plan (screen IDs M01–M06, the two-dimension health rule, presence-based
offline, probe semantics) is the governing spec; this document only designs the panels for it.

## 3. Information architecture

Stable screen IDs, used in review and future feedback:

| ID | Screen | In the prototype |
|---|---|---|
| M01 | Connection health | `#overview` — verdict, tiles, freshness, timeline, incidents, segment table. |
| M02 | Machine connections | Approximated by the *Your machine* scope of M01. Not drawn separately (see §12). |
| M03 | Cloud services | Approximated by the *Cloud service* scope of M01. Not drawn separately. |
| M04 | Request trace | `#trace/<incident>` — waterfall or table of one correlated request. |
| M05 | Incident investigation | `#incident/<incident>` — facts / suspected / missing, waterfall, code locations. |
| M06 | Monitoring status | `#monitoring` — per-source freshness, gaps, drops, retention, read-only probe. |
| — | Agent evidence | `#evidence/<incident>` — structured JSON with copy and download. |

Navigation: M01 verdict → *Open incident* → M05 → *Request trace* (M04) → *Agent evidence*. A
breadcrumb on every inner page returns to M01. The verdict links to M06.

### Desktop (≥ 900 px)

```
[ prototype label — sticky, always visible                                   ]
[ sidebar ][ fixture · scope · window · Live/Pause                           ]
[         ][ VERDICT: outcome + evidence quality + since + Open incident     ]
[         ][ incident | inbound queue | error rate | latency p50/p95/p99 + n  ]
[         ][ freshness by source (4 cards)                                   ]
[         ][ shared timeline: 6 lanes, incident band + marker buttons        ]
[         ][ incidents in window        ][ health by segment (table)         ]
```

### Phone (360 px)

Sidebar becomes a drawer behind a 44 px menu button. Tiles go two by two. Each timeline lane puts
its name *above* the track so the track keeps the full width; the page never scrolls sideways.
Tables become one card per row (the campaigns pattern, `admin.css:480-489`). The waterfall puts
the stage name and time on one line and the bar on the next. Controls wrap.

## 4. Components

Each row: purpose · data · actions · empty / loading / error / unknown.

**Prototype label.** Always on screen, sticky, accent background, localized
("設計原型 · 示意資料 · 非即時監控" / "Design prototype · illustrative data · not live
monitoring"). Holds the language toggle. In production the same strip is the *example mode*
label and appears on every view and every export.

**Controls.** Scope (Cloud service / Your machine / This viewer), window (15 min / 1 h / 6 h),
Live / Pause. Live means the window's end follows the clock and source ages keep counting; paused
freezes the window and every age and says how many receipts would have arrived since. A live
refresh never moves keyboard focus, scroll position or the selected incident. Changing scenario
(fixture only) resets the window to 1 h and the selection to that scenario's newest active
incident, and says so; a deep link to an incident that is not in the new scenario falls back to
M01 with a notice. Loading: controls stay usable; the body shows the admin skeleton
(`admin.css:443-448`). Error: see the verdict.

**Verdict.** The first thing read. Two separate answers: **outcome** (healthy, slow, blocked,
error, offline, unknown) and **evidence quality** (complete, partial, stale, not collected,
unsupported, not permitted, no requests). Plus the newest active incident, *since HH:MM UTC (N
ago)*, and *Open incident*. A blocked verdict has an accent frame; unknown has a dashed frame, so
"we cannot see" never looks like "fine". Loading: skeleton. Load failure with prior data: keep the
old verdict, dated, with the stale notice (`AdminGate.tsx:208-211`). First-load failure: no verdict
at all, the typed failure code — never a green guess.

**Tiles.** Latest active incident; machine inbound queue (rows used of the registered 64, meter,
oldest waiting age, and its as-of time when the machine is offline); error rate with its
denominator; latency p50 with p95, p99 and sample count. Below 20 samples p99 is replaced by "too
few samples (n)". Empty latency is "Unknown", never 0 ms. In a scope that cannot read owner data
the queue tile says so instead of showing a number.

**Freshness by source.** One card per source: this viewer, relay metrics, machine status snapshot,
telemetry collector. Each states fresh / stale / no receipt / not reported / not visible, the age
of the last receipt, the expected cadence, and how long it is kept. There is no single global
"connected" badge.

**Shared timeline.** One lane per segment over the selected window: browser, relay, machine
receive, machine process, machine send, collector. States are drawn by **height and pattern**, not
colour alone: healthy is a thin grey line; slow is a medium band; blocked is the tallest accent
bar; errors are a striped band; offline is a hollow capsule; unknown is diagonal hatching across
the whole track; not retained is a dotted line with words; not visible in this scope is an empty
track with the words. Point events (connection close, refusal start, reconnects) are thin ticks.
The incident interval is a shaded band across every lane; its marker is a real button (`■
INC-S1`) that opens M05. Each track has an `aria-label` that reads the lane as a sentence. *Show as
table* swaps in the same data as a table.

**Incidents in window.** Rows with glyph, title, id, active/resolved, interval, cause certainty and
an *Open* button. Empty: "No incident was opened in this window."

**Health by segment.** Table: segment, outcome, evidence quality, p50, p95, p99, samples, errors,
source age. Unknown cells say "Unknown"; forbidden rows show "—" with the quality "Not permitted".

**Incident (M05).** Header with severity and a certainty badge (cause confirmed / suspected /
unknown). A facts list (id, start, end or ongoing, segment, trace, build). Three separate columns:
**observed facts** (each with its source), **suspected cause** (with its basis: a source, a fixture
declaration or "inference"), **missing evidence**. A suspected cause built only from ordering says
so in its own words. Then the waterfall and code locations.

**Waterfall / request trace (M04).** Seven stages, each a separate receipt: accepted, queued,
executed, published, relay delivered, viewer observed, acknowledged. A stage with no receipt is a
dashed empty bar reading "No receipt" — it is never assumed to have happened. A late stage is warn
coloured **and** carries ▲. The axis is logarithmic so a 40 ms queue and a 61 s receipt are both
visible; times are printed, so the scale never has to be read. Table alternative: stage, offset,
delta from previous, recorder, receipt age.

**Code locations.** Repository path with line, symbol, and one line on why it is relevant, at the
build revision that emitted the evidence. Public repository paths only; a private service is named
by role ("private Cloud service relay"), never by path.

**Monitoring status (M06).** Per-source table: freshness, age, gaps in window, dropped records,
schema version, retention. Then the **read-only probe** panel: a synthetic read issued from this
viewer, every 60 s, at most one outstanding. It lists each edge it exercised and its result, and
lists *command execution* as "Not exercised: unverified". It is excluded from user success/error
denominators and "proves this viewer's viewpoint only".

**Agent evidence.** Pretty-printed JSON and two real local actions: *Copy JSON*
(`navigator.clipboard`; on refusal the text is selected and the person is told to press ⌘C/Ctrl+C)
and *Download .json* (a Blob generated in the page). Both status messages say the content is
generated from the fixture; there is no "captured diagnostics" message anywhere.

## 5. Health truth table

Outcome, decided per segment and then for the visible path (worst wins, in this order):

| Outcome | Evidence required (proposal) | Glyph | Must stay distinct from |
|---|---|---|---|
| Blocked | An identified in-flight stage exceeds its no-progress deadline (proposal: oldest pending > 30 s with zero completions in that stage). | ■ | A full queue alone, which is saturation. |
| Offline | The machine's authenticated presence contract at the relay expired (proposal: 90 s without renewal). | ○ | A direct-path close, which marks only that path unavailable; sleep, power and network causes are never inferred. |
| Error | Attributed typed failures for a named stage (proposal: ≥ 1 % of a window's logical operations). | ✕ | A successful relay fallback after a direct failure. |
| Slow | Valid-window p95 above the operation's target with ≥ 20 samples (proposal targets from `cloud-request-architecture-v2.md` §9). | ▲ | Timeouts, which are counted as errors and shown separately. |
| Unknown | Evidence missing, stale, contradictory or unsupported. | ? | Healthy. A gap is never filled with zero or the previous green. |
| No requests | A fresh source saw nothing to do. | – | Missing data. |
| Healthy | Fresh observations and recent completed work within target. | ✓ | Socket-open, which proves only the transport. |

Evidence quality, separately: `complete`, `partial` (gaps inside the window), `stale` (source
stopped; values shown with their as-of time), `not_collected` (no receipt at all), `unsupported`
(this build does not emit it), `forbidden` (this scope may not read it), `no_requests`.

Recovery requires new success or progress evidence; errors merely stopping leaves recovery
unverified.

## 6. Freshness and retention wording

A source is **fresh** while its last receipt is within twice its expected cadence (proposal), then
**stale**. A source that never reported in the window is **no receipt**. Wording on screen:
"Last receipt 12 s ago · expected every 15 s" and "Kept for 24 h". Proposed retention, to be set
by the integration plan's budget: viewer 24 h of hourly counts (as `direct-history.ts` keeps
today), relay metrics 24 h, machine detail the newest 50 commands (`docs/diagnostics.md:193-197`),
collector 6 h. Beyond retention the timeline says **Not retained**, which is not unknown and not
healthy.

## 7. Permission boundaries

| Scope | Who | Reads | Never reads |
|---|---|---|---|
| Cloud service | Strict superuser (the admin gate's API decision, `AdminGate.tsx:16-25`) | Relay metadata, presence, collector health, anonymous aggregates | Owner machine detail; any decrypted content. Superuser cannot decrypt a machine's data. |
| Your machine | Account owner on a paired device | Its own machine snapshot and this viewer's record, plus the Cloud-side rows for that machine | Other accounts. |
| This viewer | Any signed-in viewer | This browser's own attempts and probe | Anything else. |

The operator's own machine may compose Cloud and owner rows only when both authorizations are held
separately; linking from Devices never grants superuser rights. A lane the scope may not read says
"Not visible in this scope" and its quality is "Not permitted" — it is not unknown and not empty.
An offline machine still shows every Cloud-side observation (presence expiry, refusals,
`machine_offline` counts) and its last snapshot with an as-of time.

## 8. Critical journeys (each is a fixture in the prototype)

1. **Handler stalled, socket open.** Verdict *blocked*, queue 64/64, oldest waiting 18 min. Relay
   lane is slow (forwarded, no reply) while its socket is open; machine process lane is the tall
   accent bar. M05: cause **confirmed** because the fixture declares an injected fault; waterfall
   stops after *queued*.
2. **Relay receipt delayed.** Verdict *slow*, quality *partial* (a 6-minute relay gap). Execution
   and publish succeeded in 0.3 s; *relay delivered* is late (▲, 61 s). Cause **suspected** and
   the page says the suspicion rests on ordering only; three items of missing evidence are listed.
3. **Monitoring source unavailable.** Verdict *unknown*, quality *stale*; machine lanes hatched for
   22 minutes, while the relay still reports traffic. Nothing turns green.
4. **Machine offline.** Verdict *offline* in every scope, from presence expiry. A direct close two
   minutes earlier is shown as a tick on the browser lane and explained as path-only.
5. **Reconnect storm.** 47 reconnects in 9 minutes, now recovered; close reasons *unsupported* by
   this build, so quality stays partial and the cause stays suspected.
6. **Healthy.** Quiet page; the browser lane shows *no requests* before a viewer opened.

## 9. Accessibility

Contrast measured with the WCAG formula against `--card` (#16161a): `--ink` 14.5:1, `--dim`
6.2:1, `--accent` 5.8:1, `--warn` 5.7:1, `--adm-link` 8.5:1; `--faint` 3.4:1 and `--bad` 3.4:1
are used only for outlines, stripes and bars (≥ 3:1 non-text). Severity is always a glyph plus a
word. Every control is a native button, link, select or radio with the admin focus ring
(`admin.css:70-73`); touch targets are ≥ 32 px (incident markers) and ≥ 38–44 px elsewhere. Route
changes move focus to the page heading. Escape closes the drawer. Reduced motion removes the
drawer animation.

## 10. Integration notes for the root's technical plan

- Route: `/admin/health` with subroutes per M-screen; restore window, scope and selected incident
  from opaque ids in the URL; unauthorized deep links return a typed refusal with no data flash.
- Strings: the prototype's `WORDS` dictionary maps one-to-one onto `cloud/admin/words.ts` keys
  (English and Taiwan Traditional Chinese together, as the admin catalogue already does).
- Components to reuse rather than copy: `Tile`, `adm-panel`, `adm-table` with the phone card
  pattern, `adm-stale`, `adm-demo-control`, `adm-skeleton`. New: lane track, waterfall row,
  severity glyph, quality chip. The lane colours `--lane-ok` and `--lane-unknown` are new
  admin-local tokens (dark only, like the rest).
- **Diagnostics must stay readable when the failing relay is unusable.** M06 and the machine's own
  snapshot (`cloud-status`, `status.go`) are read through a path that does not depend on the relay
  being healthy; when that path also fails, the page shows the failed query, this viewer's probe
  viewpoint and the last confirmed data with its as-of time.
- **Monitoring is bounded.** Stage events go out in bounded batches with registered limits in
  `internal/domain/capacity`; at the limit the oldest is dropped and counted, and the drop count is
  shown in M06. It never becomes a second unbounded queue in front of the relay, and it introduces
  no paid service dependency.
- The dashboard is passive: no alerts by default, no commands, no automatic retry of a user
  mutation from an incident or a lost receipt.
- The evidence JSON shape (`clawdline.connection-evidence/0.1-draft`) is a starting point for the
  agent query contract, not a frozen schema.

## 11. Proposals left to the person

- **Light theme.** The console is dark only on purpose (`tokens.css:1-5`); this page follows it.
  A light admin would be a product-wide change, not this page's.
- **New lane tokens** (`--lane-ok`, `--lane-unknown`) in the admin palette. Cost: two tokens in
  `admin.css`; no other screen is affected.
- **Making M02 and M03 full screens** rather than scopes of M01. Cost: two more routes and their
  own layouts (presence chain, viewer/carrier table; relay admission and saturation).

## 12. Not verified

- No browser was opened by this task. The prototype was syntax-checked and every view was rendered
  in a DOM stub for each scenario × scope × window × live/paused × chart/table × language (2,376
  renders, no `undefined`, `NaN` or missing string). Visual QA at desktop and 360 px, focus order
  and screen-reader output are left to the root.
- M02 and M03 are not drawn as their own screens.
- All thresholds, cadences and retention periods are proposals; none were measured.

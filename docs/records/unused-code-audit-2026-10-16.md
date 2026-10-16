# Unused product code audit (2026-10-16)

Scope: the shipped Go daemon and React console at `070c9a0fc`, inspected by tracing page registration, static imports, API routes, Cloud operation catalogs, and current data producers. This is a source-reachability audit, not telemetry: an external API client may call a route even when this console does not. Historical design documents and the retired Swift archive are records, not live product entry points.

## Removed in this change: Project Timeline

The Timeline page had no caller of `openTimeline`. The v1 Board link had been retired, and Settings sent its "Open timeline" button to Projects while its toggle stayed disabled. Its read projected broker task and Git landing records; no deployment producer could establish availability. The removal covers the React page and copied renderer, settings block, HTTP route, Cloud read word and relay mapping, JSON schema and generated contracts, capacity row, translations, and feature tests. The route removal raises the daemon API level from 10 to 11. Broker task records and landing evidence remain where their active consumers read them. The terminal receipt timeline and schedule webhook delivery timeline are separate active features.

## Additional candidates

| Candidate | Evidence | Disposition |
| --- | --- | --- |
| Copied Project Board page renderer, `web/console/src/legacy/js/view/board.js` | It exports `bindBoardPage`; no runtime file imports or calls it. `web/console/src/legacy/board-bridge.ts` says the page is gone and serves Settings plus a Projects fallback. | Remove the renderer in a separate change after updating the legacy-copy manifest and checks. Keep `board-bridge.ts`, `GET/POST /v1/board`, and `legacy/board.css`: Settings and current pages still consume them. |
| Copied Start and Schedule input modules, `web/console/src/legacy/js/input/start.js` and `input/schedule.js` | No runtime imports of these modules. `web/console/src/legacy/schedules-bridge.ts` explicitly describes the Schedule copy as a text reference for the React implementation; `session/Start.tsx` similarly references the Start copy. | These are dormant reference copies, not active flows. Decide whether the byte-locked legacy reference corpus remains a repository policy before deleting them; their React replacements are active. |
| Project Timeline's historical docs and old inventories | `docs/timeline-design.md`, `docs/design-decisions.md`, and `docs/cutover.md` record the retired implementation and earlier decisions. Inventory snapshots still name old strings. | Keep historical records; update current status statements and regenerate inventories through their own tools. Do not present old design plans as current features. |

## Checked and retained

- The Board compatibility API and `board-bridge.ts` are used by the Settings Board block; the Projects fallback also calls `openBoard`.
- `legacy/board.css` supplies shared page styles to Work, Verify, Archive, and Terminal.
- `core/util.js` has active imports from Projects, Screen, command handling, Markdown, and other modules. An old QA note that characterized it as inactive is stale.
- The terminal receipt timeline and schedule webhook timeline have active rendering and transport paths. Their name alone is not evidence of dead code.

This audit identifies source reachability. It cannot establish that no external client calls an API route, or measure how often a visible feature is used. Those questions require usage telemetry or an API compatibility policy.

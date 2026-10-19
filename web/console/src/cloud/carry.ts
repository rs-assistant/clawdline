// What this console carries to a machine, word by word, and what it does not.
//
// The seam used to decide that in two hand-written places: a `switch` over four
// GET paths in `relay-reader.ts` and a table of routes in `relay-writer.ts`.
// Both were right when they were written and neither could notice when they
// stopped being: the machine's vocabulary is `cloudops.Implemented()` on the Go
// daemon, it grows, and nothing here read it. That is how `info` came to be a
// word the machine answers and the browser never asks — the status line under
// every cloud session said "Loading…" for as long as that was true, and the
// machine's log held no refusal, because the refusal was this page's own.
//
// So the words live in one place, the vocabulary table
// (internal/domain/cloudvocab/vocabulary.json), and reach this console as
// three lists that together are exactly the machine's vocabulary:
//
//   `CARRIED`           — asked of the machine, with the console route that asks.
//   `DEFERRED`          — the machine answers it; this console does not ask
//                         yet, and says why and where it can be done instead.
//   `NO_MACHINE_ROUTE`  — in the machine's vocabulary with nothing behind it
//                         there.
//
// **The lists are generated, not written here.** `tools/contract-gen` renders
// them into `carry-generated.ts` from each row's `console`, `console_route`
// and `console_sentence`, and `go run ./tools/contract-gen -check` fails while
// that file and the table disagree. The table is held to the machine by
// `TestTheVocabularyTableIsTheBridgesCatalog` (internal/app/cloudops): a word
// the machine registers with no row, or a row whose class says the machine
// answers it when it does not, fails there by name. Adding a word is therefore
// a row in the table and the route that asks for it, and nothing else. What
// stays here is what the console does with the lists. The runtime half is
// `RelayReader.drift()`, which asks the machine's own descriptor the same
// question on the page, for a machine running ahead of the bundle it is being
// read from.
//
// **A sentence in `DEFERRED` is a decision, and a decision expires.** `git`
// sat there saying the working tree was not read over Cloud yet, months after
// this machine began answering the word and after `legacy/git-bridge.ts` had
// been copied in and was asking for the route on every press of 「Git 變更」;
// the page refused its own request and the person was told "無法讀取 Git 變更".
// `schedules` sat there the same way and for the same length of time. Neither
// was caught, because the guard only ever asked whether a word was
// *classified*, never whether the sentence was still true. `DEFERRED_ASKED` is
// the half of that question a machine can answer.
//
// Only the generated lists are imported here, and they import nothing, so both
// the console and `node --test` read this file as it is.

// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { CARRIED, DEFERRED, NO_MACHINE_ROUTE } from "./carry-generated.ts"
import type { CloudWord } from "./carry-generated.js"

export { CARRIED, DEFERRED, NO_MACHINE_ROUTE }
export type CarriedWord = keyof typeof CARRIED
export type { CloudWord }

/** The overview carries exactly one period and one opaque project, never arbitrary query fields. */
export function usageOverviewQuery(params: URLSearchParams): { days: "7" | "28"; project?: string } | null {
  const seen = new Set<string>()
  for (const [key] of params) {
    if ((key !== "days" && key !== "project") || seen.has(key)) return null
    seen.add(key)
  }
  const days = params.get("days") ?? "7"
  const project = params.get("project") ?? ""
  if (days !== "7" && days !== "28") return null
  if (/[\u0000-\u001f\u007f]/u.test(project)) return null
  return { days, ...(project ? { project } : {}) }
}

/**
 * The deferred words a screen in this console already asks for.
 *
 * **This is what "deliberately not carried yet" costs, named.** A word is in
 * `DEFERRED` because somebody decided not to carry it; it is in this list
 * because some page here issues the request anyway, so the refusal a person
 * meets is this bundle refusing itself, in front of a screen that was built to
 * show the answer. Deferring is still legal — the list is not a failure — but
 * it is no longer silent, and this is the roster to re-read when choosing what
 * to carry next.
 *
 * It is held to both directions by `carry.test.ts`, which reads every `/v1/…`
 * path this console spells and asks `uncarriedWordOf` what each one stands
 * for: a deferred word some screen asks for and this list does not name fails,
 * and so does a name here that no screen asks for any more. That is the guard
 * `git` needed and did not have — the day `git-bridge.ts` landed, the entry
 * had to say so.
 */
export const DEFERRED_ASKED: readonly (keyof typeof DEFERRED)[] = []

/**
 * Own-origin routes this seam answers itself, with no Cloud word behind them.
 *
 * They are not part of the machine's vocabulary and so not part of the Go drift
 * guard; they are here so that the one place that says what a hosted page can
 * reach says all of it, and `carry.test.ts` holds the reader to this list so
 * that being here is a fact and not a note. `/v1/sessions` is the relay's own
 * snapshot of this machine's rows, `/v1/orchestrator/tasks` is the dispatched
 * work that rides on the same machine's descriptor, `/v1/health` is whether
 * the line is up, and `/v1/strings` is the bundle's own catalog
 * (`cloud/strings.ts`).
 *
 * **The task list is here and not in a word list**, and that is the whole
 * answer to "which class does it belong to". A word is something a browser
 * asks a machine to do; this is something the machine already said. It publishes its
 * dispatched work on the `orch/` snapshot beside `machine.commands`
 * (`internal/transport/cloud/tasklist.go`), the copied client keeps every
 * descriptor it opens, and reading it back costs the relay nothing — exactly
 * as the session rows do. `dispatch` stays in `NO_MACHINE_ROUTE` and is a
 * different thing: it is *starting* a task from a browser, which this machine
 * refuses by name (`cloudDispatchUnpinned`) because no pinned wire shape says
 * where the task file would be written. Reading the list was never that.
 */
export const ANSWERED_HERE: readonly string[] = [
  "/v1/sessions",
  "/v1/orchestrator/tasks",
  "/v1/health",
  "/v1/strings",
]

/**
 * HTTP routes this machine answers only to itself and paired devices. The
 * hosted terminal uses its separate encrypted channels, not this generic
 * Cloud Router.
 *
 * **Why a fourth list and not one of the three.** Those three are the
 * machine's Cloud vocabulary, held to `Implemented()` and `Vocabulary()` by the
 * Go drift guard, and a terminal has no word there on purpose: a word would be
 * a thing the relay could be asked to carry. Nor is it `ANSWERED_HERE`, which
 * is what this seam answers. So without this list a terminal route fell to the
 * last sentence of `notCarriedDetail` — "do it on the machine itself" — which
 * is true and hides the reason: this HTTP route is not carried. The machine
 * says the same with its own refusal,
 * `terminal_cloud_not_supported`, for a request that reaches it anyway
 * (`internal/transport/http/terminals.go`).
 */
export const MACHINE_ONLY: readonly { readonly route: RegExp; readonly sentence: string }[] = [
  {
    route: /^\/v1\/terminals(\/|$)/,
    sentence: "Terminal HTTP routes are local to this machine or a paired device. Hosted terminals use separate encrypted Cloud channels.",
  },
]

/** The sentence for a route only the machine itself or a paired device may use, or "". */
export function machineOnly(path: string): string {
  const bare = path.split("?")[0]
  for (const { route, sentence } of MACHINE_ONLY) {
    if (route.test(bare)) return sentence
  }
  return ""
}

/**
 * What the seam is handed: the words this bundle asks for, and what it says
 * about a route it does not carry.
 *
 * `relay-reader.ts` imports nothing at run time — that is what lets it be
 * loaded as it is by `node --test` — so the table reaches it as a value rather
 * than as an import (`RelayReaderOptions.carry`), from the one place that
 * assembles the seam.
 */
export interface CarryTable {
  readonly carried: readonly string[]
  detail(method: string, path: string, word?: string): string
}

/** This build's table, as the seam takes it. */
export const CARRY_TABLE: CarryTable = { carried: Object.keys(CARRIED), detail: notCarriedDetail }

/** Every word in the three lists, sorted — what the machine's vocabulary must be. */
export function words(): CloudWord[] {
  return [...Object.keys(CARRIED), ...Object.keys(DEFERRED), ...Object.keys(NO_MACHINE_ROUTE)].sort() as CloudWord[]
}

/**
 * The Cloud word a console route this seam does not carry stands for, or "".
 *
 * Only the uncarried ones: what is carried is parsed by `writeRoute` and by
 * the reader's own cases, and a second spelling of those would be a second
 * thing to keep right. This is what turns "this is not carried" into "snippets
 * are not carried", which is the difference between a refusal somebody can act
 * on and one they can only report.
 */
export function uncarriedWordOf(method: string, path: string): string {
  const parts = path.split("/").slice(1)
  if (parts[0] !== "v1") return ""
  let segments: string[]
  try {
    segments = parts.slice(1).map((p) => decodeURIComponent(p))
  } catch {
    return ""
  }
  const [head, a, b] = segments
  // `/v1/snippets*`, `board` and `board-command` were all here until
  // this console began asking for them. What is carried is parsed once — by
  // the reader's own case and by `writeRoute` — and a second spelling of it
  // here would be a second thing to keep right. The Settings page's POST to
  // `/v1/board` is parsed by `writeRoute` as `board-command`, so it is not a
  // word this table has to name either.
  if (head === "diagnostics" && a === "report") return "diagnostics.report"
  // The schedule list, single read and writes are carried, so they are parsed
  // once by the reader's own cases and `writeRoute`, and are deliberately not
  // spelled a second time here.
  if (head === "sessions" && a && b) {
    switch (b) {
      case "skills":
        return "skills"
    }
  }
  return ""
}

/** The sentence for a word this console will not ask for, or "" for one it carries. */
export function uncarried(word: string): string {
  const deferred = (DEFERRED as Record<string, string>)[word]
  if (deferred) return deferred
  return (NO_MACHINE_ROUTE as Record<string, string>)[word] ?? ""
}

/**
 * What a refusal says about a route this console does not carry.
 *
 * A route that stands for one of the machine's words says that word's sentence, so
 * the reader learns what is missing rather than that "something" is. A route
 * with no word at all — most of this daemon's API — says so plainly. Neither
 * is read for its wording by anything: the code is `cloud_not_carried` and the
 * screens choose their own sentence by it (`legacy/js/core/failure-text.js`).
 */
export function notCarriedDetail(method: string, path: string, word?: string): string {
  const only = machineOnly(path)
  if (only) return only
  const sentence = uncarried(word || uncarriedWordOf(method, path))
  if (sentence) return sentence
  return `${method} ${path} is not carried over Clawdline Cloud: do it on the machine itself.`
}

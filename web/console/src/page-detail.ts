/**
 * One record opened on a sidebar page, as an address: `#page=<page>&id=<id>`.
 *
 * A page that shows a list and one record of it at a time (驗收, Projects,
 * Timeline, Documents) used to hold the open record in its own state only, so
 * the browser's Back left the whole page, and a reload or a link could not name
 * the record. Opening one now adds one history step carrying its address, and
 * closing it from the page goes back over that step, so the page's own back
 * control and the browser's Back mean the same thing.
 *
 * There is only ever one step per page. Opening a second record from an open
 * one replaces the step; an address that arrived naming a record (a reload, a
 * link) had no list under it, so the entry it arrived in becomes the list and
 * the record is pushed over it. The step is marked in `history.state`, so a
 * page never goes Back over a step it did not add.
 */

const MARK = "page-detail"

interface Step {
  view: typeof MARK
  page: string
  id: string
}

function fieldInHash(hash: string, name: string): string {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
  const found = new RegExp(`(?:^|[#&])${escaped}=([^&]*)`).exec(String(hash || ""))
  if (!found || !found[1]) return ""
  try {
    return decodeURIComponent(found[1])
  } catch {
    return found[1]
  }
}

/** The record an address names on `page`, or "" when it names none (or another page). */
export function detailInHash(page: string, hash: string): string {
  if (fieldInHash(hash, "page") !== page) return ""
  return fieldInHash(hash, "id").trim()
}

/** The address of one record on a page. */
export function detailHash(page: string, id: string): string {
  return `#page=${encodeURIComponent(page)}&id=${encodeURIComponent(id)}`
}

/** The address of the page's list. */
export function listHash(page: string): string {
  return `#page=${encodeURIComponent(page)}`
}

/** The history a page moves through; a parameter so a test can hold its own. */
export interface DetailHistory {
  readonly state: unknown
  pushState(state: unknown, unused: string, url: string): void
  replaceState(state: unknown, unused: string, url: string): void
  back(): void
}

function ownStep(history: DetailHistory, page: string): boolean {
  const state = history.state as Partial<Step> | null
  return state?.view === MARK && state.page === page
}

/** Open `id` on `page`: one step over the list, or the step already there replaced. */
export function enterDetail(page: string, id: string, history: DetailHistory = window.history): void {
  const step: Step = { view: MARK, page, id }
  try {
    if (ownStep(history, page)) history.replaceState(step, "", detailHash(page, id))
    else history.pushState(step, "", detailHash(page, id))
  } catch {
    /* the address stays as it was */
  }
}

/**
 * The address arrived naming `id` with no step of this page under it: the
 * entry becomes the list and the record is pushed over it, so Back from the
 * record is the list rather than whatever came before the console.
 */
export function arriveAtDetail(page: string, id: string, history: DetailHistory = window.history): void {
  if (ownStep(history, page)) return
  try {
    history.replaceState(history.state, "", listHash(page))
    history.pushState({ view: MARK, page, id } satisfies Step, "", detailHash(page, id))
  } catch {
    /* the address stays as it was */
  }
}

/**
 * Close the record. Over a step this page added, that is Back — the caller
 * hears it as `popstate` like the browser's own; otherwise the address is
 * put back to the list in place and the caller closes the record itself.
 * Answers whether it went Back.
 */
export function leaveDetail(page: string, history: DetailHistory = window.history): boolean {
  if (ownStep(history, page)) {
    history.back()
    return true
  }
  try {
    history.replaceState(history.state, "", listHash(page))
  } catch {
    /* the address stays as it was */
  }
  return false
}

/**
 * Coming back to a page that still holds an open record (the drawer wrote the
 * bare page address): the address names the record again, in place.
 */
export function restateDetail(page: string, id: string, history: DetailHistory = window.history): void {
  try {
    history.replaceState(history.state, "", detailHash(page, id))
  } catch {
    /* the address stays as it was */
  }
}

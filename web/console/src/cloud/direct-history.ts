// What this browser's direct attempts to each machine came to, hour by hour.
//
// The direct carrier is one channel between this page and one machine
// (`direct-carrier.ts`), so whether it opens is a fact about *this* browser's
// network and that machine's: another browser of the same account can succeed
// where this one cannot. That is why the record is kept here, in this browser,
// and nowhere else — the machine's own log names attempts from every viewer.
//
// The record answers one question on the machine sheet: since when has the
// direct path stopped opening? It keeps counts, not attempts: an hour holds how
// many tries opened and how many did not, and nothing about the network.
//
// Bounds (`internal/domain/capacity`, console.direct_history_*): 24 hours per
// machine, 32 machines per browser. An hour past the window is dropped when the
// record is next written; past the machine bound, the machine whose newest hour
// is oldest goes.

export const HISTORY_HOURS = 24
export const HISTORY_MACHINES = 32
const HOUR_MS = 3_600_000
const KEY = "clawdline.direct-history.v1"

/** One hour of attempts. `hour` is the hour's start, in whole hours since the epoch. */
export interface DirectHour { hour: number; ok: number; failed: number }

/** Where the record lives. A test passes a map; a page that cannot store keeps it in memory. */
export interface HistoryStore {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

type Record_ = Record<string, DirectHour[]>

function browserStore(): HistoryStore | null {
  try { return typeof localStorage === "undefined" ? null : localStorage } catch { return null }
}

let memory: Record_ = {}
const listeners = new Set<() => void>()

function read(store: HistoryStore | null): Record_ {
  if (!store) return memory
  try {
    const parsed: unknown = JSON.parse(store.getItem(KEY) ?? "{}")
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {}
    const out: Record_ = {}
    for (const [machine, hours] of Object.entries(parsed as Record<string, unknown>)) {
      if (!Array.isArray(hours)) continue
      out[machine] = hours.filter((h): h is DirectHour =>
        !!h && Number.isSafeInteger(h.hour) && Number.isSafeInteger(h.ok) && Number.isSafeInteger(h.failed))
    }
    return out
  } catch {
    return {}
  }
}

function write(store: HistoryStore | null, record: Record_): void {
  if (!store) { memory = record; return }
  try { store.setItem(KEY, JSON.stringify(record)) } catch { memory = record }
}

/** Count one attempt to `machine`. */
export function recordDirectAttempt(machine: string, opened: boolean, now = Date.now(), store = browserStore()): void {
  if (typeof machine !== "string" || !machine) return
  const hour = Math.floor(now / HOUR_MS)
  const record = read(store)
  const hours = (record[machine] ?? []).filter((h) => h.hour > hour - HISTORY_HOURS)
  let current = hours.find((h) => h.hour === hour)
  if (!current) { current = { hour, ok: 0, failed: 0 }; hours.push(current) }
  if (opened) current.ok++
  else current.failed++
  hours.sort((a, b) => a.hour - b.hour)
  record[machine] = hours
  const machines = Object.keys(record)
  if (machines.length > HISTORY_MACHINES) {
    const newest = (m: string) => record[m]!.reduce((at, h) => Math.max(at, h.hour), -Infinity)
    machines.sort((a, b) => newest(a) - newest(b))
    for (const gone of machines.slice(0, machines.length - HISTORY_MACHINES)) delete record[gone]
  }
  write(store, record)
  for (const listener of [...listeners]) listener()
}

/** The last 24 hours for `machine`, oldest first; an hour with no attempt counts zero of each. */
export function directHistory(machine: string, now = Date.now(), store = browserStore()): DirectHour[] {
  const hour = Math.floor(now / HOUR_MS)
  const held = new Map((read(store)[machine] ?? []).map((h) => [h.hour, h]))
  return Array.from({ length: HISTORY_HOURS }, (_, i) => {
    const at = hour - HISTORY_HOURS + 1 + i
    return held.get(at) ?? { hour: at, ok: 0, failed: 0 }
  })
}

/**
 * The start of the run of failures that reaches the present, or null when the
 * newest hour with any attempt had one that opened. Hours with no attempt do
 * not break the run: nothing needed a carrier then, which says nothing either way.
 */
export function failingSince(hours: readonly DirectHour[]): number | null {
  let since: number | null = null
  for (let i = hours.length - 1; i >= 0; i--) {
    const h = hours[i]!
    if (h.ok > 0) break
    if (h.failed > 0) since = h.hour * HOUR_MS
  }
  return since
}

/** Called after every recorded attempt, so an open sheet redraws. */
export function watchDirectHistory(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

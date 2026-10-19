import type { UsageOverview, UsageOverviewDay } from "./api.js"

export type UsageMetric = "tokens" | "dispatches" | "duration"

export function dailyValue(day: UsageOverviewDay | undefined, metric: UsageMetric): number | null {
  if (!day) return null
  if (metric === "duration") return day.duration_samples > 0 && day.duration_median_seconds !== null ? day.duration_median_seconds / 60 : null
  return metric === "tokens" ? day.tokens : day.dispatches
}

/** Adjacent missing points split a path; they never acquire an invented baseline. */
export function lineSegments(values: readonly (number | null)[], maximum: number, width: number): string[] {
  const paths: string[] = []
  let path = ""
  values.forEach((value, index) => {
    if (value === null) { if (path) paths.push(path); path = ""; return }
    const x = 54 + index * (width - 78) / Math.max(1, values.length - 1)
    const y = 174 - value / maximum * 150
    path += `${path ? " L" : "M"}${x.toFixed(2)},${y.toFixed(2)}`
  })
  if (path) paths.push(path)
  return paths
}

export function comparisonComplete(data: UsageOverview): boolean {
  return data.coverage.state === "complete" && !data.coverage.truncated && data.daily.length === data.days
    && data.previous_daily.length === data.days
    && [...data.daily, ...data.previous_daily].every(day => day.coverage === "complete")
}

export function percentageChange(current: number | null, previous: number | null, complete: boolean): string | null {
  if (!complete || current === null || previous === null || previous === 0) return null
  const percent = (current - previous) / previous * 100
  return `${percent > 0 ? "+" : ""}${Math.round(percent)}%`
}

export function compactValue(value: number | null): string {
  if (value === null) return "—"
  if (value >= 1_000_000_000) return `${Number((value / 1_000_000_000).toFixed(1))}B`
  if (value >= 1_000_000) return `${Number((value / 1_000_000).toFixed(1))}M`
  if (value >= 1_000) return `${Number((value / 1_000).toFixed(1))}K`
  return Number(value.toFixed(1)).toLocaleString()
}

/** Calendar labels come from the daemon; never reinterpret YYYY-MM-DD in the browser zone. */
export function shortDate(date: string): string { return date.slice(5).replace("-", "/") }

export function selectedDay(index: number, key: string, count: number): number {
  if (key === "Home") return 0
  if (key === "End") return Math.max(0, count - 1)
  return Math.max(0, Math.min(count - 1, index + (key === "ArrowLeft" ? -1 : key === "ArrowRight" ? 1 : 0)))
}

/** A superseded request cannot publish success or failure, even when transport ignores abort. */
export class UsageRequestScope {
  private controller: AbortController | null = null
  begin(): AbortSignal { this.cancel(); this.controller = new AbortController(); return this.controller.signal }
  cancel(): void { this.controller?.abort(); this.controller = null }
  current(signal: AbortSignal): boolean { return signal === this.controller?.signal && !signal.aborted }
}

/** Keep calendar endpoints explicit, repeating the year only across a year boundary. */
export function dateRange(first: string | undefined, last: string | undefined): string {
  if (!first || !last) return first ?? last ?? ""
  return `${first} – ${first.slice(0, 4) === last.slice(0, 4) ? last.slice(5) : last}`
}

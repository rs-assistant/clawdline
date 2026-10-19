// Temporary view contract; integration can reexport the generated wire types.
export interface UsageOverviewTotals {
  tokens: number | null
  dispatches: number
  duration_median_seconds: number | null
  duration_samples: number
  parts: { input: number; cache_read: number; cache_write: number; output: number }
}
export interface UsageOverviewDay {
  date: string
  tokens: number | null
  dispatches: number
  duration_median_seconds: number | null
  duration_samples: number
  coverage: "missing" | "partial" | "complete"
}
export interface UsageOverview {
  at: number
  days: 7 | 28
  timezone: string
  since: number
  until: number
  previous_since: number
  previous_until: number
  project: string
  totals: UsageOverviewTotals
  previous_totals: UsageOverviewTotals
  daily: UsageOverviewDay[]
  previous_daily: UsageOverviewDay[]
  projects: { id: string; name: string; tokens: number }[]
  project_options: { id: string; name: string }[]
  coverage: { state: "missing" | "partial" | "complete"; sessions_read: number; sessions_total: number; history_from: number; truncated: boolean }
}

export class UsageReadError extends Error {
  readonly code: string
  constructor(code: string) { super(code); this.code = code }
}

/** Same-origin fetch is carried by cloud/install, already pinned to the selected machine. */
export async function readUsageOverview(days: 7 | 28, project: string, signal: AbortSignal): Promise<UsageOverview> {
  const query = new URLSearchParams({ days: String(days) })
  if (project) query.set("project", project)
  const response = await fetch("/v1/usage/overview" + "?" + query, { credentials: "same-origin", signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]) })
  const body = await response.json()
  if (!response.ok) throw new UsageReadError(body?.error?.code ?? body?.code ?? (typeof body?.error === "string" ? body.error : undefined) ?? (response.status === 404 || response.status === 501 ? "not_implemented" : "read_failed"))
  return { ...body, daily: body.daily ?? [], previous_daily: body.previous_daily ?? [], projects: body.projects ?? [], project_options: body.project_options ?? [] } as UsageOverview
}

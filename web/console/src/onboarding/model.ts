export const REASONS = ["phone", "sessions", "agents", "machines", "notifications", "exploring", "other"] as const
export const TOOLS = ["claude", "codex", "terminal_other", "web_desktop", "not_started"] as const
export const SOURCES = ["github", "search", "ai", "friend", "social", "video_podcast", "article", "event", "other"] as const
export interface Answers { reason: string; tools: string[]; source: string }
export interface SurveyState { version: 1; revision: number; status: "new" | "shown" | "skipped" | "submitted"; answers: Answers | null }
export interface LocalSurvey { respondent: string; state: SurveyState; dismissed: boolean; shownAt?: string; skippedAt?: string; submittedAt?: string; pending?: { key: string; body: Record<string, unknown> } }
export const LOCAL_KEY = "clawdline.onboarding.v1"
export const OPEN_SURVEY = "clawdline:open-survey"
export function validAnswers(a: unknown): a is Answers {
  if (!a || typeof a !== "object") return false
  const x = a as Answers
  return REASONS.includes(x.reason as typeof REASONS[number]) && SOURCES.includes(x.source as typeof SOURCES[number]) &&
    Array.isArray(x.tools) && x.tools.length > 0 && x.tools.length <= TOOLS.length &&
    x.tools.every(t => TOOLS.includes(t as typeof TOOLS[number])) && new Set(x.tools).size === x.tools.length &&
    (!x.tools.includes("not_started") || x.tools.length === 1)
}
export function toggleTool(tools: string[], tool: string): string[] {
  if (tools.includes(tool)) return tools.filter(t => t !== tool)
  return tool === "not_started" ? [tool] : [...tools.filter(t => t !== "not_started"), tool]
}
export function parseState(value: unknown): SurveyState {
  const v = value as SurveyState | null
  if (!v || v.version !== 1 || !Number.isSafeInteger(v.revision) || v.revision < 0 ||
    !["new", "shown", "skipped", "submitted"].includes(v.status) ||
    (v.status === "submitted" ? !validAnswers(v.answers) : v.answers !== null)) throw new Error("survey_invalid_response")
  return v
}
export function readLocal(storage: Pick<Storage, "getItem">): LocalSurvey {
  try {
    const value = JSON.parse(storage.getItem(LOCAL_KEY) ?? "null") as LocalSurvey | null
    if (value && /^[0-9a-f-]{36}$/i.test(value.respondent) && typeof value.dismissed === "boolean") {
      const { pending: draft, ...saved } = value
      const pending = draft && typeof draft.key === "string" && /^[\x20-\x7e]{1,200}$/.test(draft.key) &&
        draft.body && typeof draft.body === "object" && validAnswers(draft.body.answers) ? draft : undefined
      return { ...saved, state: parseState(value.state), ...(pending ? { pending } : {}) }
    }
  } catch { /* A browser with no usable storage can still fill out the form. */ }
  return { respondent: crypto.randomUUID(), state: { version: 1, revision: 0, status: "new", answers: null }, dismissed: false }
}
export async function surveyRequest(origin: string, path: string, options: { body?: Record<string, unknown>; key?: string; local?: boolean; signal?: AbortSignal } = {}): Promise<SurveyState> {
  const response = await fetch(origin + path, {
    method: options.body ? "POST" : "GET", credentials: options.local ? "omit" : "include", cache: "no-store", signal: options.signal,
    headers: options.body ? { "content-type": "application/json", "idempotency-key": options.key! } : undefined,
    body: options.body ? JSON.stringify(options.body) : undefined,
  })
  const value = await response.json() as { error?: { code?: string; details?: { current?: unknown } } }
  if (!response.ok) {
    const error = new Error(typeof value.error?.code === "string" ? value.error.code : "survey_unavailable")
    if (value.error?.code === "survey_revision_conflict" && value.error.details?.current) Object.assign(error, { current: parseState(value.error.details.current) })
    throw error
  }
  return parseState(value)
}

// The Projects page's way into its copied modules.
//
// `js/view/projects.js` and `js/view/worktrees.js` are the Swift app's, byte for
// byte. They are bound here as that app's `main.js` binds them: one table of the
// page's elements by id, and an environment whose reads are this daemon's routes
// spelled as the original's `api` spells them (`net/live.js`).
//
// What differs, and why:
//
// - The list reads `GET /v1/places`, the current directory catalog. The
//   retired Board store never decides whether a Project is present.
// - `openWorktreeOwner` is absent: the original opens the owner in the Board's
//   session viewer, which this console does not have. The owner button is still
//   drawn, as the module draws it, and does nothing.
// - A refusal on this daemon is `{ error: "code", detail }`, not
//   `{ error: { code, message } }`; `jsonFetch` below reads both, and — as the
//   original's — sets no `status`, so the sentences that append one do not.
import { T } from "./js/core/i18n.js"
import { machineWording } from "./machine-copy.js"
import { drawIcon } from "./js/core/pixels.js"
import { tint } from "./js/core/util.js"
import { makeJSONFetch } from "@clawdline/core/refusal"
import { bindProjectsPage as bindProjectsPageOriginal } from "./js/view/projects.js"
import { renderWorktreeRows } from "./js/view/worktrees.js"
import { lifecyclePlace } from "./lifecycle-place"

let projectReadRefusal: unknown = null
let projectReadAttempt = 0
let worktreeReadRefusal: unknown = null
let worktreeReadAttempt = 0

/** The copied renderer consumes the error code; its host may also show the explicit wire detail. */
export const latestProjectReadRefusal = (): unknown => projectReadRefusal
export const latestWorktreeReadRefusal = (): unknown => worktreeReadRefusal

async function rememberWorktreeRead<T>(read: () => Promise<T>): Promise<T> {
  const attempt = ++worktreeReadAttempt
  worktreeReadRefusal = null
  try { return await read() }
  catch (error) {
    if (attempt === worktreeReadAttempt) worktreeReadRefusal = error
    throw error
  }
}

/** `net/client.js`'s LOCAL_MACHINE: a page served by this daemon is looking at this machine. */
const LOCAL_MACHINE = "this-mac"

/** What `bindProjectsPage` hands back. */
export interface ProjectsPage {
  /** The list; with `wanted`, then the Project of that place id once the list holds it. */
  enter(wanted?: string): Promise<void>
  leave(): void
  escape(): void
  /** The address now names this Project ("" for the list): open or close to match it. */
  follow(id: string): Promise<void>
  state: { view: "list" | "detail" }
}

/** `main.js`'s element table for `bindProjectsPage`, in its order. */
export const PROJECTS_ELEMENT_IDS = [
  "projects",
  "projects-list-view",
  "projects-detail-view",
  "projects-title", "projects-count",
  "projects-status", "projects-rows",
  "projects-back",
  "project-mark", "project-name",
  "project-path", "project-status",
  "project-truncated",
  "project-delivered",
  "project-delivered-count",
  "project-delivered-title",
  "project-delivered-say",
  "project-delivered-list",
  "project-delivered-none",
  "project-none", "project-groups",
  "project-excluded",
  "project-unattributed",
  "project-unattributed-title",
  "project-unattributed-say",
  "project-read",
  "project-worktree-lifecycle",
  "project-worktree-status",
  "project-worktree-summary",
  "project-worktree-rows",
  "project-worktree-refresh",
] as const

/** `net/fetch.js`'s `jsonFetch`, now supplied by the shared refusal-aware transport. */
const jsonFetch = makeJSONFetch({
  words: { offline: machineWording(T.webOffline, "en"), requestFailed: T.webRequestFailed, notJSON: T.webNotJSON },
})

type Place = { id: string; projectId?: string; path?: string; machine?: string }

export interface WorktreeSourceRow {
  worktreeId?: string
  path?: string
  branch?: string
  classifications?: string[]
  status?: { complete?: boolean; staged?: number | null; modified?: number | null; untracked?: number | null }
  target?: string
  owner?: { title?: string; evidence?: string } | null
  context?: {
    purpose?: string; note?: string; currentStatus?: string; state?: string
    createdAt?: string; startedAt?: string; finishedAt?: string
    originSession?: { title?: string }
  } | null
  cleanup?: { eligible?: boolean; blockers?: Array<{ code?: string }> } | null
}
let latestWorktreeRows = new Map<string, WorktreeSourceRow>()
let worktreeProgressGeneration = 0

export function worktreeSourceRow(id: string): WorktreeSourceRow | undefined {
  return latestWorktreeRows.get(id)
}

export async function removeWorktree(project: string, worktree: string): Promise<void> {
  await jsonFetch("/v1/projects/" + encodeURIComponent(project) + "/worktrees/" + encodeURIComponent(worktree),
    { method: "DELETE" })
}

const post = (body: unknown): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
})

/** The transport `readProjectPlaces` reads, as `net/live.js` spells it. */
const transport = {
  places: (machine?: string) => {
    if (machine !== undefined && machine !== LOCAL_MACHINE) {
      return Promise.reject(Object.assign(new Error("This machine is not available."), { code: "machine_unavailable" }))
    }
    return jsonFetch("/v1/places")
  },
  projectWorktreeLifecycle: async (project: string) => {
    const answer = await jsonFetch("/v1/projects/" + encodeURIComponent(project) + "/worktrees")
    const rows = (answer.projectWorktreeLifecycle as { rows?: WorktreeSourceRow[] } | undefined)?.rows
    latestWorktreeRows = new Map(Array.isArray(rows) ? rows.filter(row => row?.worktreeId).map(row => [row.worktreeId!, row]) : [])
    return { ...answer, machine: LOCAL_MACHINE }
  },
  projectWorktreeLifecycleRefresh: async (project: string) => {
    const answer = await jsonFetch("/v1/projects/" + encodeURIComponent(project) + "/worktrees/refresh", post({}))
    const rows = (answer.projectWorktreeLifecycle as { rows?: WorktreeSourceRow[] } | undefined)?.rows
    latestWorktreeRows = new Map(Array.isArray(rows) ? rows.filter(row => row?.worktreeId).map(row => [row.worktreeId!, row]) : [])
    return { ...answer, machine: LOCAL_MACHINE }
  },
}

/**
 * Bind the page once its markup is in the document, with `main.js`'s
 * environment less the two surfaces this console does not have.
 */
/** How an opened Project moves the history (`page-detail.ts`). */
export interface ProjectsAddress {
  enter(id: string): void
  /** True when it went Back, which is heard as the address changing. */
  leave(): boolean
}

export function bindProjects(doc: Document, navigate: (name: string) => void, address?: ProjectsAddress): ProjectsPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of PROJECTS_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  async function refreshWithProgress(place: Place) {
    const project = place.projectId || place.id
    const generation = ++worktreeProgressGeneration
    let reading = false
    let finished = false
    const timer = setInterval(async () => {
      if (reading || finished || generation !== worktreeProgressGeneration ||
          elements["project-worktree-lifecycle"]?.hidden) return
      reading = true
      try {
        const answer = await transport.projectWorktreeLifecycle(project)
        const snapshot = (answer as { projectWorktreeLifecycle?: unknown }).projectWorktreeLifecycle as {
          schemaVersion?: number; project?: { id?: string }; error?: { code?: string }
          rows?: WorktreeSourceRow[]
        } | undefined
        if (finished || generation !== worktreeProgressGeneration ||
            elements["project-worktree-lifecycle"]?.hidden || snapshot?.schemaVersion !== 1 ||
            snapshot.project?.id !== project || snapshot.error?.code !== "observation_in_progress" ||
            !Array.isArray(snapshot.rows)) return
        const rows = elements["project-worktree-rows"]
        if (rows) renderWorktreeRows(rows, snapshot.rows, { document: doc, language: doc.documentElement.lang })
        const status = elements["project-worktree-status"]
        if (status) status.textContent = /^zh/i.test(doc.documentElement.lang)
          ? "正在逐一觀測工作樹…" : "Observing worktrees one by one…"
      } catch { /* The final refresh reports failures. */ }
      finally { reading = false }
    }, 1500)
    try {
      return await rememberWorktreeRead(() => transport.projectWorktreeLifecycleRefresh(project))
    } finally {
      finished = true
      clearInterval(timer)
    }
  }
  return (bindProjectsPageOriginal as (elements: Record<string, HTMLElement | null>, environment: Record<string, unknown>) => ProjectsPage)(
    elements,
    {
      carries: () => true,
      places: async () => {
        const attempt = ++projectReadAttempt
        projectReadRefusal = null
        try {
          const answer = await transport.places()
          return { ...answer, places: (answer.places as Place[]).map(lifecyclePlace) }
        } catch (error) {
          if (attempt === projectReadAttempt) projectReadRefusal = error
          throw error
        }
      },
      lifecycleAvailable: () => true,
      projectWorktreeLifecycle: (place: Place) => {
        ++worktreeProgressGeneration
        return rememberWorktreeRead(() => transport.projectWorktreeLifecycle(place.projectId || place.id))
      },
      projectWorktreeLifecycleRefresh: refreshWithProgress,
      drawIcon,
      tint,
      navigate,
      address,
    },
  )
}

/** `static.js`'s three words for this page, painted once the catalog is in. */
export function paintProjectsStatic(doc: Document): void {
  const text = (id: string, value: string | undefined) => {
    const node = doc.getElementById(id)
    if (node && value) node.textContent = value
  }
  const strings = T as Record<string, string>
  text("projects-title", strings.webProjects)
  text("projects-lede", strings.webProjectsLede)
  if (strings.webProjects) text("projects-back", "‹ " + strings.webProjects)
}

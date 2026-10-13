import { useEffect, useRef, useState } from "react"
import type { Snippet } from "../legacy/snippets-bridge.js"
import { readProjectPlaces, type ProjectPlace } from "./work/api.js"
import { listProjectFiles, type ProjectFile } from "./projects/project-files-api.js"
import { createWorkflow, deleteWorkflow, readReusableTextForMachine, SnippetFailure, updateWorkflow } from "../session/snippets-api.js"
import type { PageModule } from "./types.js"
import "./workflow/workflow.css"
import { workflowWord as W } from "../workflow-words.js"

type Draft = { id: string; title: string; body: string }
const blank = (): Draft => ({ id: "", title: "", body: "" })
const machineOf = (place: ProjectPlace): string => (place as ProjectPlace & { machine?: string }).machine ?? ""

/** A Workflow is saved text. Skill import fills an editor, not an execution dependency. */
function WorkflowPage({ shown }: { shown: boolean }) {
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [place, setPlace] = useState<ProjectPlace | null>(null)
  const [rows, setRows] = useState<Snippet[]>([])
  const [skills, setSkills] = useState<ProjectFile[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const editor = useRef<HTMLDivElement>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  const openDraft = (next: Draft) => {
    if (!draft) returnFocus.current = document.activeElement as HTMLElement | null
    setDraft(next)
  }

  useEffect(() => {
    if (!draft) return
    editor.current?.querySelector<HTMLInputElement>("input")?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); setDraft(null); return }
      if (event.key !== "Tab") return
      const controls = Array.from(editor.current?.querySelectorAll<HTMLElement>("input, textarea, button:not(:disabled)") ?? [])
      if (!controls.length) return
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => { document.removeEventListener("keydown", onKey); returnFocus.current?.focus(); returnFocus.current = null }
  }, [!!draft])

  useEffect(() => {
    if (!shown) return
    let live = true
    setLoading(true)
    readProjectPlaces().then((answer) => {
      if (!live) return
      setPlaces(answer.places ?? [])
      setPlace((current) => answer.places.find((candidate) => candidate.id === current?.id) ?? answer.places[0] ?? null)
      setError("")
    }).catch(() => { if (live) setError(W("projectUnavailable")) })
      .finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [shown])

  const refresh = async (selected: ProjectPlace) => {
    const [answer, files] = await Promise.all([
      readReusableTextForMachine(machineOf(selected)),
      listProjectFiles(selected.id),
    ])
    setRows((answer.snippets ?? []).filter((row) => row.scope === "workflow" && row.project === selected.path))
    setSkills(files.files.filter((file) => file.kind === "skill" && file.source === "project" && file.status === "ready"))
  }

  useEffect(() => {
    if (!shown || !place) return
    let live = true
    setLoading(true)
    Promise.all([readReusableTextForMachine(machineOf(place)), listProjectFiles(place.id)])
      .then(([answer, files]) => {
        if (!live) return
        setRows((answer.snippets ?? []).filter((row) => row.scope === "workflow" && row.project === place.path))
        setSkills(files.files.filter((file) => file.kind === "skill" && file.source === "project" && file.status === "ready"))
        setError("")
      }).catch(() => { if (live) setError(W("workflowUnavailable")) })
      .finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [shown, place])

  const save = async () => {
    if (!place || !draft || busy) return
    const title = draft.title.trim()
    const body = draft.body.trim()
    if (!title || !body) { setError(W("needsText")); return }
    if (new TextEncoder().encode(title).length > 200 || new TextEncoder().encode(body).length > 4000) {
      setError(W("tooLong")); return
    }
    setBusy(true); setError("")
    try {
      if (draft.id) await updateWorkflow(machineOf(place), draft.id, title, body)
      else await createWorkflow(machineOf(place), place.path, title, body)
      await refresh(place)
      setDraft(null)
    } catch (cause) { setError(cause instanceof SnippetFailure && cause.code === "snippet_limit_reached" ? W("limitReached")
      : cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("saveRefused") : W("saveUnknown")) }
    finally { setBusy(false) }
  }

  const remove = async () => {
    if (!place || !draft?.id || busy) return
    if (!window.confirm(W("confirmDelete", { name: draft.title }))) return
    setBusy(true); setError("")
    try { await deleteWorkflow(machineOf(place), draft.id); await refresh(place); setDraft(null) }
    catch (cause) { setError(cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("deleteRefused") : W("deleteUnknown")) }
    finally { setBusy(false) }
  }

  const importSkill = (id: string) => {
    const skill = skills.find((file) => file.id === id)
    if (!skill) return
    const name = skill.location.split("/").filter(Boolean).at(-2) ?? skill.name
    openDraft({ id: "", title: name, body: W("skillPrompt", { name: skill.location }) })
  }

  return <section className="page workflow-page" id="workflow" data-page-view="workflow" hidden={!shown}>
    <header className="workflow-head"><h1 tabIndex={-1}>Workflow</h1>
      <button type="button" onClick={() => place && void refresh(place)} disabled={!place || loading || busy}>{W("reload")}</button>
    </header>
    <p>{W("intro")}</p>
    <label className="workflow-project">Project
      <select value={place?.id ?? ""} onChange={(event) => { setDraft(null); setPlace(places.find((row) => row.id === event.target.value) ?? null) }}>
        {places.map((row) => <option key={row.id} value={row.id}>{row.label || row.path}</option>)}
      </select>
    </label>
    {loading && <p role="status">{W("loading")}</p>}
    {error && <p role="alert" className="workflow-error">{error}</p>}
    {place && <>
      <div className="workflow-actions"><button type="button" onClick={() => openDraft(blank())}>{W("new")}</button>
        <label>{W("fromSkill")}
          <select value="" onChange={(event) => importSkill(event.target.value)}>
            <option value="">{W("chooseSkill")}</option>
            {skills.map((skill) => <option key={skill.id} value={skill.id}>{skill.location}</option>)}
          </select>
        </label>
      </div>
      {!loading && rows.length === 0 && <p>{W("empty")}</p>}
      <div className="workflow-list">{rows.map((row) => <button type="button" key={row.id}
        onClick={() => openDraft({ id: row.id, title: row.title, body: row.body })}>
        <strong>{row.title}</strong><span>{row.body}</span>
      </button>)}</div>
    </>}
    {draft && <div ref={editor} className="workflow-editor" role="dialog" aria-modal="true" aria-label={draft.id ? W("edit") : W("new")}>
      <form onSubmit={(event) => { event.preventDefault(); void save() }}>
        <h2>{draft.id ? W("edit") : W("new")}</h2>
        <label>{W("name")}<input autoFocus value={draft.title} maxLength={200} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
        <label>{W("message")}<textarea value={draft.body} rows={8} maxLength={4000} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /></label>
        <div className="workflow-actions"><button type="submit" disabled={busy}>{W("save")}</button>
          <button type="button" disabled={busy} onClick={() => setDraft(null)}>{W("cancel")}</button>
          {draft.id && <button type="button" disabled={busy} onClick={() => void remove()}>{W("delete")}</button>}</div>
      </form>
    </div>}
  </section>
}

export const page: PageModule = { id: "workflow", requiresApiLevel: 1, Component: WorkflowPage }

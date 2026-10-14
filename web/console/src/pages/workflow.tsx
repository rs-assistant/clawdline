import { useEffect, useRef, useState } from "react"
import type { Snippet } from "../legacy/snippets-bridge.js"
import { readProjectPlaces, type ProjectPlace } from "./work/api.js"
import { listProjectFiles, type ProjectFile } from "./projects/project-files-api.js"
import { createWorkflow, deleteWorkflow, readReusableTextForMachine, SnippetFailure, updateWorkflow } from "../session/snippets-api.js"
import type { PageModule } from "./types.js"
import "./workflow/workflow.css"
import { workflowWord as W } from "../workflow-words.js"
import { Mark } from "../session/List.js"
import type { SessionRow } from "@clawdline/contract"

type Draft = { id: string; title: string; body: string; global: boolean }
const blank = (): Draft => ({ id: "", title: "", body: "", global: false })
const machineOf = (place: ProjectPlace): string => (place as ProjectPlace & { machine?: string }).machine ?? ""
const availableSkills = (files: ProjectFile[]) => files.filter((file) => file.kind === "skill" && file.status === "ready")

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
  const [projectOpen, setProjectOpen] = useState(false)
  const projectPicker = useRef<HTMLDivElement>(null)
  const editor = useRef<HTMLDivElement>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  const openDraft = (next: Draft) => {
    if (!draft) returnFocus.current = document.activeElement as HTMLElement | null
    setError("")
    setDraft(next)
  }

  useEffect(() => {
    if (!draft) return
    editor.current?.querySelector<HTMLElement>("form")?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); setDraft(null); return }
      if (event.key !== "Tab") return
      const controls = Array.from(editor.current?.querySelectorAll<HTMLElement>("input, textarea, button:not(:disabled)") ?? [])
      if (!controls.length) return
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && (document.activeElement === first || document.activeElement === editor.current?.querySelector("form"))) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => { document.removeEventListener("keydown", onKey); returnFocus.current?.focus(); returnFocus.current = null }
  }, [!!draft])

  useEffect(() => {
    if (!projectOpen) return
    const closeOutside = (event: PointerEvent) => {
      if (!projectPicker.current?.contains(event.target as Node)) setProjectOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === "Escape") setProjectOpen(false) }
    document.addEventListener("pointerdown", closeOutside)
    document.addEventListener("keydown", closeOnEscape)
    return () => { document.removeEventListener("pointerdown", closeOutside); document.removeEventListener("keydown", closeOnEscape) }
  }, [projectOpen])

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
    try {
      const [answer, files] = await Promise.all([
        readReusableTextForMachine(machineOf(selected)),
        listProjectFiles(selected.id),
      ])
      setRows((answer.snippets ?? []).filter((row) => row.scope === "workflow" && (!row.project || row.project === selected.path)))
      setSkills(availableSkills(files.files))
      setError("")
      return true
    } catch { setError(W("workflowUnavailable")); return false }
  }

  useEffect(() => {
    if (!shown || !place) return
    let live = true
    setLoading(true)
    setSkills([])
    Promise.all([readReusableTextForMachine(machineOf(place)), listProjectFiles(place.id)])
      .then(([answer, files]) => {
        if (!live) return
        setRows((answer.snippets ?? []).filter((row) => row.scope === "workflow" && (!row.project || row.project === place.path)))
        setSkills(availableSkills(files.files))
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
      else await createWorkflow(machineOf(place), draft.global ? "" : place.path, title, body)
      if (await refresh(place)) setDraft(null)
    } catch (cause) { setError(cause instanceof SnippetFailure && cause.code === "snippet_limit_reached" ? W("limitReached")
      : cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("saveRefused") : W("saveUnknown")) }
    finally { setBusy(false) }
  }

  const remove = async () => {
    if (!place || !draft?.id || busy) return
    if (!window.confirm(W("confirmDelete", { name: draft.title }))) return
    setBusy(true); setError("")
    try { await deleteWorkflow(machineOf(place), draft.id); if (await refresh(place)) setDraft(null) }
    catch (cause) { setError(cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("deleteRefused") : W("deleteUnknown")) }
    finally { setBusy(false) }
  }

  const importSkill = (id: string) => {
    const skill = skills.find((file) => file.id === id)
    if (!skill) return
    const name = skill.location.split("/").filter(Boolean).at(-2) ?? skill.name
    openDraft({ id: "", title: name, body: W("skillPrompt", { name: skill.location }), global: false })
  }

  const skillPicker = <label className="workflow-skill-picker"><span>{W("importSkill")}</span>
    <select aria-label={W("chooseSkill")} value="" onChange={(event) => importSkill(event.target.value)} disabled={!skills.length}>
      <option value="">{W("chooseSkill")}</option>
      {skills.map((skill) => <option key={skill.id} value={skill.id}>{skill.location}</option>)}
    </select>
    {!skills.length && !loading && <small>{W("noSkills")}</small>}
  </label>

  return <section className="page workflow-page" id="workflow" data-page-view="workflow" hidden={!shown}>
    <header className="workflow-head"><div><h1 tabIndex={-1}>Workflow</h1><p>{W("intro")}</p></div>
      <div className="workflow-head-tools"><div className="workflow-project" ref={projectPicker}><span>{W("projectLabel")}</span>
        <button type="button" className="workflow-project-trigger" aria-label={`${W("projectLabel")}: ${place?.label || place?.path || ""}`} aria-expanded={projectOpen}
          onClick={() => setProjectOpen(!projectOpen)}>{place && <Mark icon={place.icon as SessionRow["icon"]} cellPx={3} />}
          <span>{place?.label || place?.path || W("projectLabel")}</span><span aria-hidden="true">⌄</span></button>
        {projectOpen && <div className="workflow-project-menu">{places.map((row) => <button
          type="button" aria-current={row.id === place?.id ? "true" : undefined} key={row.id} onClick={() => {
            setDraft(null); setPlace(row); setProjectOpen(false)
          }}><Mark icon={row.icon as SessionRow["icon"]} cellPx={3} /><span>{row.label || row.path}</span>
          {row.id === place?.id && <span aria-hidden="true">✓</span>}</button>)}</div>}
      </div>
        <button className="workflow-page-reload" type="button" onClick={() => place && void refresh(place)} disabled={!place || loading || busy}>{W("reload")}</button>
      </div>
    </header>
    {loading && <p role="status">{W("loading")}</p>}
    {error && !draft && <p role="alert" className="workflow-error">{error}</p>}
    {place && <>
      {!loading && rows.length > 0 && <div className="workflow-toolbar"><h2>{W("workflowCount")} <span>{rows.length}</span></h2>
        <div className="workflow-actions"><button className="workflow-primary" type="button" onClick={() => openDraft(blank())}>{W("new")}</button>{skillPicker}</div>
      </div>}
      {!loading && rows.length === 0 && <div className="workflow-empty"><div className="workflow-empty-mark" aria-hidden="true">↗</div>
        <h2>{W("emptyTitle")}</h2><p>{W("emptyDetail")}</p>
        <div className="workflow-actions"><button className="workflow-primary" type="button" onClick={() => openDraft(blank())}>{W("new")}</button>{skillPicker}</div>
      </div>}
      <div className="workflow-list">{rows.map((row) => <button type="button" key={row.id}
        onClick={() => openDraft({ id: row.id, title: row.title, body: row.body, global: !row.project })}>
        <span className="workflow-list-copy"><strong>{row.title} <small>{!row.project ? W("global") : W("projectOnly")}</small></strong><span>{row.body}</span></span><span className="workflow-edit-label">{W("editAction")} <span aria-hidden="true">→</span></span>
      </button>)}</div>
    </>}
    {draft && <div ref={editor} className="workflow-editor" role="dialog" aria-modal="true" aria-label={draft.id ? W("edit") : W("new")}
      onPointerDown={(event) => { if (event.target === event.currentTarget && !busy) setDraft(null) }}>
      <form tabIndex={-1} onSubmit={(event) => { event.preventDefault(); void save() }}>
        <h2>{draft.id ? W("edit") : W("new")}</h2>
        <label>{W("name")}<input value={draft.title} maxLength={200} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
        {!draft.id && <fieldset className="workflow-scope"><legend>{W("availability")}</legend>
          <label><input type="radio" name="workflow-scope" checked={!draft.global} onChange={() => setDraft({ ...draft, global: false })} />
            {place && <Mark icon={place.icon as SessionRow["icon"]} cellPx={3} />}<span>{place?.label || place?.path}</span></label>
          <label><input type="radio" name="workflow-scope" checked={draft.global} onChange={() => setDraft({ ...draft, global: true })} />
            <span>{W("global")}</span></label>
        </fieldset>}
        {draft.global && <p className="workflow-scope-hint">{W("globalHint")}</p>}
        <label>{W("message")}<textarea value={draft.body} rows={8} maxLength={4000} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /></label>
        {error && <p role="alert" className="workflow-error">{error}</p>}
        <div className="workflow-actions"><button className="workflow-primary" type="submit" disabled={busy}>{W("save")}</button>
          <button type="button" disabled={busy} onClick={() => setDraft(null)}>{W("cancel")}</button>
          {draft.id && <button className="workflow-delete" type="button" disabled={busy} onClick={() => void remove()}>{W("delete")}</button>}</div>
      </form>
    </div>}
  </section>
}

export const page: PageModule = { id: "workflow", requiresApiLevel: 1, Component: WorkflowPage }

import { useEffect, useRef, useState } from "react"
import type { Snippet, WorkflowSource } from "../legacy/snippets-bridge.js"
import { readProjectPlaces, type ProjectPlace } from "./work/api.js"
import { listProjectFiles, readProjectFile, type ProjectFile } from "./projects/project-files-api.js"
import { createWorkflow, deleteWorkflow, readReusableTextForMachine, SnippetFailure, updateWorkflow } from "../session/snippets-api.js"
import type { PageModule } from "./types.js"
import "./workflow/workflow.css"
import { workflowWord as W } from "../workflow-words.js"
import { Mark } from "../session/List.js"
import type { SessionRow } from "@clawdline/contract"

type Draft = { id: string; title: string; body: string; global: boolean; source: WorkflowSource | null }
const blank = (): Draft => ({ id: "", title: "", body: "", global: false, source: null })
const machineOf = (place: ProjectPlace): string => (place as ProjectPlace & { machine?: string }).machine ?? ""
const availableSkills = (files: ProjectFile[]) => files.filter((file) => (file.kind === "skill" || file.kind === "command") && file.status === "ready")
const commandName = (file: ProjectFile) => file.name.replace(/\.md$/i, "")
const skillTitle = (file: ProjectFile) => file.kind === "command" && (!file.title || file.title === commandName(file))
  ? commandName(file).replace(/[-_]+/g, " ").replace(/\b[a-z]/g, (letter) => letter.toUpperCase())
  : file.title || file.location.split("/").filter(Boolean).at(-2) || file.name

/** A Workflow owns its launch text; source attribution points to an optional original. */
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
  const [skillOpen, setSkillOpen] = useState(false)
  const [selectedSkill, setSelectedSkill] = useState<ProjectFile | null>(null)
  const [skillText, setSkillText] = useState("")
  const [skillVersion, setSkillVersion] = useState("")
  const [linkingDraft, setLinkingDraft] = useState(false)
  const [sourcePreview, setSourcePreview] = useState<WorkflowSource | null>(null)
  const [sourceText, setSourceText] = useState("")
  const [sourceVersion, setSourceVersion] = useState("")
  const [sourceError, setSourceError] = useState("")
  const [sourceBusy, setSourceBusy] = useState(false)
  const sourceRequest = useRef(0)
  const sourceReturnFocus = useRef<HTMLElement | null>(null)
  const sourceDialog = useRef<HTMLDivElement>(null)
  const [skillBusy, setSkillBusy] = useState(false)
  const [skillError, setSkillError] = useState("")
  const skillRequest = useRef(0)
  const skillDialog = useRef<HTMLDivElement>(null)
  const skillReturnFocus = useRef<HTMLElement | null>(null)
  const projectPicker = useRef<HTMLDivElement>(null)
  const editor = useRef<HTMLDivElement>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  const openDraft = (next: Draft) => {
    if (!draft) returnFocus.current = document.activeElement as HTMLElement | null
    setError("")
    setDraft(next)
  }
  const closeSkill = () => {
    skillRequest.current++
    setSkillOpen(false); setSelectedSkill(null); setSkillText(""); setSkillVersion(""); setSkillError(""); setSkillBusy(false)
  }
  const chooseSkill = async (skill: ProjectFile) => {
    if (!place) return
    const request = ++skillRequest.current
    setSelectedSkill(skill); setSkillText(""); setSkillError(""); setSkillBusy(true)
    try {
      const answer = await readProjectFile(place.id, skill.id)
      if (request === skillRequest.current) { setSkillText(answer.text); setSkillVersion(answer.version) }
    } catch { if (request === skillRequest.current) setSkillError(W("skillReadFailed")) }
    finally { if (request === skillRequest.current) setSkillBusy(false) }
  }
  useEffect(() => {
    if (!skillOpen) return
    skillDialog.current?.querySelector<HTMLElement>("button")?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); closeSkill(); return }
      if (event.key !== "Tab") return
      const controls = Array.from(skillDialog.current?.querySelectorAll<HTMLElement>("button:not(:disabled)") ?? [])
      if (!controls.length) return
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => { document.removeEventListener("keydown", onKey); skillReturnFocus.current?.focus(); skillReturnFocus.current = null }
  }, [skillOpen])
  useEffect(() => { closeSkill() }, [place?.id, shown])

  const showSource = async (source: WorkflowSource) => {
    sourceReturnFocus.current = document.activeElement as HTMLElement | null
    const request = ++sourceRequest.current
    setSourcePreview(source); setSourceText(""); setSourceVersion(""); setSourceError(""); setSourceBusy(true)
    try {
      const answer = await readProjectFile(source.place_id, source.file_id)
      if (answer.file.kind !== source.kind) throw new Error("source changed")
      if (request === sourceRequest.current) { setSourceText(answer.text); setSourceVersion(answer.version) }
    } catch { if (request === sourceRequest.current) setSourceError(W("sourceUnavailable")) }
    finally { if (request === sourceRequest.current) setSourceBusy(false) }
  }
  const closeSource = () => {
    sourceRequest.current++
    setSourcePreview(null)
    sourceReturnFocus.current?.focus()
    sourceReturnFocus.current = null
  }
  useEffect(() => {
    if (!sourcePreview) return
    const closeButton = sourceDialog.current?.querySelector<HTMLElement>("header button")
    closeButton?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); closeSource() }
      if (event.key === "Tab" && closeButton) { event.preventDefault(); closeButton.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [sourcePreview])

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
      if (draft.id) await updateWorkflow(machineOf(place), draft.id, title, body, draft.source)
      else await createWorkflow(machineOf(place), draft.global ? "" : place.path, title, body, draft.source ?? undefined)
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

  const importSkill = () => {
    if (!selectedSkill || !skillText || !skillVersion || skillBusy || !place) return
    const name = skillTitle(selectedSkill)
    const source: WorkflowSource = { kind: selectedSkill.kind as "skill" | "command", file_id: selectedSkill.id, place_id: place.id, scope: selectedSkill.source, name, version: skillVersion }
    closeSkill()
    if (linkingDraft && draft) setDraft({ ...draft, source })
    else openDraft({ id: "", title: name, body: selectedSkill.kind === "command" ? W("commandPrompt", { name: commandName(selectedSkill) }) : W("skillPrompt", { name }), global: false, source })
    setLinkingDraft(false)
  }

  const skillPicker = <div className="workflow-skill-picker">
    <button type="button" disabled={!skills.length} onClick={() => {
      skillReturnFocus.current = document.activeElement as HTMLElement | null
      setLinkingDraft(false); setSkillOpen(true)
    }}>{W("importSkill")}</button>
    {!skills.length && !loading && <small>{W("noSkills")}</small>}
  </div>

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
      <div className="workflow-list">{rows.map((row) => <div className="workflow-list-card" key={row.id}>
        <button type="button" className="workflow-list-main"
          onClick={() => openDraft({ id: row.id, title: row.title, body: row.body, global: !row.project, source: row.source ?? null })}>
          <span className="workflow-list-copy"><strong>{row.title} <small>{!row.project ? W("global") : W("projectOnly")}</small></strong><span>{row.body}</span></span><span className="workflow-edit-label">{W("editAction")} <span aria-hidden="true">→</span></span>
        </button>{row.source && <button type="button" className="workflow-list-source" onClick={() => void showSource(row.source!)}>
          {W("sourceLabel")}: {row.source.name} ↗</button>}
      </div>)}</div>
    </>}
    {draft && <div ref={editor} className="workflow-editor" role="dialog" aria-modal="true" aria-label={draft.id ? W("edit") : W("new")}
      onPointerDown={(event) => { if (event.target === event.currentTarget && !busy) setDraft(null) }}>
      <form tabIndex={-1} onSubmit={(event) => { event.preventDefault(); void save() }}>
        <h2>{draft.id ? W("edit") : W("new")}</h2>
        <div className="workflow-source-row"><span>{W("sourceLabel")}</span>{draft.source ? <>
          <button type="button" onClick={() => void showSource(draft.source!)}>{draft.source.kind === "command" ? W("claudeCommand") : "Skill"} · {draft.source.name} ↗</button>
          <button type="button" onClick={() => setDraft({ ...draft, source: null })}>{W("unlinkSource")}</button>
        </> : <span>{W("noSource")}</span>}
          <button type="button" onClick={() => { skillReturnFocus.current = document.activeElement as HTMLElement | null; setLinkingDraft(true); setSkillOpen(true) }} disabled={!skills.length}>{W("linkSource")}</button>
        </div>
        <label>{W("name")}<input value={draft.title} maxLength={200} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
        {!draft.id && <fieldset className="workflow-scope"><legend>{W("availability")}</legend>
          <label><input type="radio" name="workflow-scope" checked={!draft.global} onChange={() => setDraft({ ...draft, global: false })} />
            {place && <Mark icon={place.icon as SessionRow["icon"]} cellPx={3} />}<span>{place?.label || place?.path}</span></label>
          <label><input type="radio" name="workflow-scope" checked={draft.global} onChange={() => setDraft({ ...draft, global: true })} />
            <span>{W("global")}</span></label>
        </fieldset>}
        {draft.global && <p className="workflow-scope-hint">{draft.source?.scope === "project" ? W("globalSourceHint") : W("globalHint")}</p>}
        <label>{W("message")}<textarea value={draft.body} rows={8} maxLength={4000} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /></label>
        {error && <p role="alert" className="workflow-error">{error}</p>}
        <div className="workflow-actions"><button className="workflow-primary" type="submit" disabled={busy}>{W("save")}</button>
          <button type="button" disabled={busy} onClick={() => setDraft(null)}>{W("cancel")}</button>
          {draft.id && <button className="workflow-delete" type="button" disabled={busy} onClick={() => void remove()}>{W("delete")}</button>}</div>
      </form>
    </div>}
    {skillOpen && <div ref={skillDialog} className="workflow-skill-overlay" role="dialog" aria-modal="true" aria-labelledby="workflow-skill-dialog-title"
      onPointerDown={(event) => { if (event.target === event.currentTarget) closeSkill() }}>
      <div className="workflow-skill-dialog">
        <header><div><h2 id="workflow-skill-dialog-title">{selectedSkill ? selectedSkill.kind === "command" ? W("commandPreviewTitle") : W("skillPreviewTitle") : W("skillListTitle")}</h2>
          {selectedSkill && <p>{skillTitle(selectedSkill)}</p>}</div><button type="button" onClick={closeSkill} aria-label={W("cancel")}>×</button></header>
        {selectedSkill ? <>
          <div className="workflow-skill-content">
            <p className="workflow-skill-description">{selectedSkill.description || W("skillNoDescription")}</p>
            {skillBusy && <p role="status">{W("loading")}</p>}
            {skillError && <p role="alert" className="workflow-error">{skillError}</p>}
            {skillText && <pre>{skillText}</pre>}
          </div>
          <footer><button type="button" onClick={() => { skillRequest.current++; setSelectedSkill(null); setSkillText(""); setSkillError(""); setSkillBusy(false) }}>{W("backToSkills")}</button>
            {skillError && <button type="button" onClick={() => void chooseSkill(selectedSkill)}>{W("reload")}</button>}
            <button className="workflow-primary" type="button" disabled={!skillText || skillBusy} onClick={importSkill}>{W(linkingDraft ? "linkSource" : "useSkill")}</button></footer>
        </> : <div className="workflow-skill-content"><div className="workflow-skill-options">{skills.map((skill) => <button type="button" key={skill.id} onClick={() => void chooseSkill(skill)}>
          <span><strong>{skillTitle(skill)}</strong><small>{skill.source === "global" ? W("global") : W("projectOnly")} · {skill.kind === "command" ? W("claudeCommand") : skill.assistant}</small></span>
          <span className="workflow-skill-summary">{skill.description || W("skillNoDescription")}</span>
        </button>)}</div></div>}
        {!selectedSkill && <footer><button type="button" onClick={closeSkill}>{W("cancel")}</button></footer>}
        {selectedSkill && <p className="workflow-skill-hint">{linkingDraft ? W("linkHint") : W(selectedSkill.kind === "command" ? "commandImportHint" : "skillImportHint")}</p>}
      </div>
    </div>}
    {sourcePreview && <div className="workflow-skill-overlay" role="dialog" aria-modal="true" aria-label={W("sourceDetail")}
      onPointerDown={(event) => { if (event.target === event.currentTarget) closeSource() }}>
      <div className="workflow-skill-dialog" ref={sourceDialog}><header><div><h2>{W("sourceDetail")}</h2><p>{sourcePreview.kind === "command" ? W("claudeCommand") : "Skill"} · {sourcePreview.name}</p></div>
        <button type="button" onClick={closeSource} aria-label={W("cancel")}>×</button></header>
        <div className="workflow-skill-content">{sourceBusy && <p role="status">{W("loading")}</p>}
          {sourceError && <p role="alert" className="workflow-error">{sourceError}</p>}
          {sourceText && <>{sourceVersion !== sourcePreview.version && <p role="status">{W("sourceChanged")}</p>}<pre>{sourceText}</pre></>}</div>
      </div>
    </div>}
  </section>
}

export const page: PageModule = { id: "workflow", requiresApiLevel: 1, Component: WorkflowPage }

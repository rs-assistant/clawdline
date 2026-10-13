import { catalogFormat, catalogWord, catalogWordLanguage, currentCatalogTag } from "../catalog.js"
import { createPortal } from "react-dom"
import { ProjectTools } from "./projects/ProjectTools.js"
import { ProjectFolderPicker } from "./projects/ProjectFolderPicker.js"
import type { ProjectSetupHandle } from "./projects/ProjectSetup.js"
import { localizePinnedProjectStatus, projectOpenLabel } from "./projects/project-list.js"
import { decoratePinnedWorktree } from "./projects/pinned-worktree-copy.js"
import { useLayoutEffect, useRef, useState } from "react"
import type { PageModule } from "./types.js"
import { bindProjects, latestProjectReadRefusal, latestWorktreeReadRefusal, removeLandedWorktree, worktreeSourceRow, type ProjectsPage } from "../legacy/projects-bridge.js"
import { machineWording } from "../legacy/machine-copy.js"
import { ActionConfirm, Info, requestPage, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./projects/section.html?raw"
import { nextWord, type NextWord } from "../next-strings.js"
import { workPageHash } from "../page-route.js"
import { openTerminalPage } from "./terminal/navigate.js"
import { showProjectRefusalDetail } from "./projects/refusal-detail.js"
import { removeProjectPlace } from "./work/api.js"
import { failureSentence } from "../legacy/bridge.js"
import { arriveAtDetail, detailInHash, enterDetail, leaveDetail } from "../page-detail.js"
import "../page-bar.css"

/** An opened Project is `#page=projects&id=<place id>` (page-detail.ts). */
const PROJECTS_ADDRESS = {
  enter: (id: string) => enterDetail("projects", id),
  leave: () => leaveDetail("projects"),
}

type ProjectTarget = {
  id: string
  projectId?: string
  label?: string
  path?: string
  activeItemCount?: number
}
type BoundProjects = ProjectsPage & {
  state: ProjectsPage["state"] & { places?: ProjectTarget[] | null; place?: ProjectTarget | null }
  openLifecycle(project: ProjectTarget): Promise<void>
}

/**
 * The Projects page: `section#projects` in the Swift app's `index.html`,
 * drawn by its `view/projects.js` and `view/worktrees.js`.
 *
 * The fragment beside this file is that section's markup between its own
 * tags, whitespace included, and the copied modules fill it — the rows, the
 * Project's identity, the worktree lifecycle cards, the delivered block and
 * its groups are the original's own DOM. React owns the section element and
 * nothing inside it, and never re-renders inside it.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter` on arrival (the list, every time), `leave` on departure, the
 * keyboard lands on the heading (`focus: "projects-title"`), and the app's
 * static words are painted for the chosen language.
 *
 * Escape: in the original, `input/keys.js` gives this page its turn after the
 * drawer and the keyboard card, and a Project open inside the page is given
 * back before the page is left. App's key handler leaves the page directly, so
 * the page's turn is taken here, ahead of it, and only when nothing App would
 * close first is open.
 */
function ProjectsPageView({ shown }: { shown: boolean }) {
  const [toolsHost, setToolsHost] = useState<HTMLElement | null>(null)
  useLayoutEffect(() => { setToolsHost(document.getElementById("project-tools-host")) }, [])
  const page = useRef<BoundProjects | null>(null)
  const setup = useRef<ProjectSetupHandle>(null)
  const was = useRef(false)
  const painted = useRef(false)
  const [removeTarget, setRemoveTarget] = useState<ProjectTarget | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [removeBusy, setRemoveBusy] = useState(false)
  const [removeError, setRemoveError] = useState("")
  const removeDialog = useRef<HTMLDialogElement>(null)
  const [worktreeTarget, setWorktreeTarget] = useState<{ id: string; path: string; project: ProjectTarget } | null>(null)
  const [worktreeBusy, setWorktreeBusy] = useState(false)
  const [worktreeError, setWorktreeError] = useState("")
  const worktreeDialog = useRef<HTMLDialogElement>(null)

  useLayoutEffect(() => {
    const button = document.getElementById("projects-add")
    if (!button) return
    button.textContent = catalogWord("projects", "addButton")
    const open = () => setAddOpen(true)
    button.addEventListener("click", open)
    return () => button.removeEventListener("click", open)
  }, [])

  useLayoutEffect(() => {
    const dialog = removeDialog.current
    if (!dialog) return
    if (removeTarget && !dialog.open) dialog.showModal()
    if (!removeTarget && dialog.open) dialog.close()
  }, [removeTarget])

  async function confirmRemove() {
    if (!removeTarget?.path || removeBusy) return
    setRemoveBusy(true)
    setRemoveError("")
    try {
      await removeProjectPlace({ id: removeTarget.id, label: removeTarget.label || removeTarget.path,
        path: removeTarget.path })
      setRemoveTarget(null)
      void page.current?.enter()
    } catch (error) {
      setRemoveError(failureSentence(error, nextWord("projectsRemoveFailed")))
    } finally {
      setRemoveBusy(false)
    }
  }

  useLayoutEffect(() => {
    const dialog = worktreeDialog.current
    if (!dialog) return
    if (worktreeTarget && !dialog.open) dialog.showModal()
    if (!worktreeTarget && dialog.open) dialog.close()
  }, [worktreeTarget])

  async function confirmWorktreeRemove() {
    if (!worktreeTarget || worktreeBusy) return
    setWorktreeBusy(true)
    setWorktreeError("")
    try {
      await removeLandedWorktree(worktreeTarget.project.projectId || worktreeTarget.project.id, worktreeTarget.id)
      const project = worktreeTarget.project
      setWorktreeTarget(null)
      await page.current?.openLifecycle(project)
    } catch (error) {
      setWorktreeError(failureSentence(error, nextWord("worktreeRemoveFailed")))
    } finally {
      setWorktreeBusy(false)
    }
  }

  useLayoutEffect(() => {
    const root = document.getElementById("project-worktree-lifecycle")
    if (!root) return
    const adapt = () => {
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const value = node.textContent ?? ""
        if (!/The Mac reports|until the Mac|Refreshing the Mac observation|Mac 回報|Mac 取得|重新觀測 Mac/.test(value)) continue
        const wording = machineWording(value, document.documentElement.lang)
        if (wording !== value) node.textContent = wording
      }
    }
    adapt()
    const observer = new MutationObserver(adapt)
    observer.observe(root, { childList: true, characterData: true, subtree: true })
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    const bound = page.current ?? (bindProjects(document, navigate, PROJECTS_ADDRESS) as BoundProjects)
    page.current = bound
    const openWork = (project: ProjectTarget) => {
      const path = typeof project.path === "string" ? project.path.trim() : ""
      if (!path) return
      const address = workPageHash(path, "projects")
      try {
        history.replaceState(history.state, "", address)
      } catch {
        location.hash = address
      }
      requestPage({ page: "work", hash: false })
    }
    // The copied renderer owns its click listener. Its ordinary branch opens
    // retired usage detail, so capture the row and open the repository lifecycle
    // instead. Work is a separate action inside that Project's detail.
    const rows = document.getElementById("projects-rows")
    const onProject = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row[data-place-id]")
      if (!button || !rows?.contains(button)) return
      const project = bound.state.places?.find((place) => place.id === button.dataset.placeId)
      if (!project?.path) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      void bound.openLifecycle(project)
    }
    const onWork = () => {
      const project = bound.state.place
      if (project) openWork(project)
    }
    const work = document.getElementById("project-open-work")
    rows?.addEventListener("click", onProject, true)
    work?.addEventListener("click", onWork)
    return () => {
      rows?.removeEventListener("click", onProject, true)
      work?.removeEventListener("click", onWork)
    }
  }, [])

  useLayoutEffect(() => {
    const lifecycle = document.getElementById("project-worktree-lifecycle")
    if (!lifecycle) return
    const decorate = () => {
      decoratePinnedWorktree(lifecycle, worktreeSourceRow)
      showProjectRefusalDetail(lifecycle.querySelector<HTMLElement>("#project-worktree-status"), latestWorktreeReadRefusal())
      for (const card of lifecycle.querySelectorAll<HTMLElement>(".worktree-card[data-worktree-id]")) {
        const existing = card.querySelector(".worktree-remove")
        const id = card.dataset.worktreeId || ""
        const row = worktreeSourceRow(id)
        const clean = row?.status?.complete === true && row.status.staged === 0
          && row.status.modified === 0 && row.status.untracked === 0
        if (!row?.cleanup?.eligible || !clean || !row.classifications?.includes("landed_identical_residue")) {
          existing?.remove()
          continue
        }
        if (existing) continue
        card.querySelector(".worktree-cleanup-assessment")?.remove()
        const button = document.createElement("button")
        button.type = "button"
        button.className = "worktree-remove"
        button.dataset.worktreeId = id
        button.textContent = nextWord("worktreeRemoveButton")
        card.appendChild(button)
      }
    }
    const onRemove = (event: Event) => {
      const button = (event.target as Element | null)?.closest<HTMLButtonElement>("button.worktree-remove[data-worktree-id]")
      if (!button || !lifecycle.contains(button)) return
      const project = page.current?.state.place
      const row = worktreeSourceRow(button.dataset.worktreeId || "")
      if (!project || !row?.worktreeId || !row.path) return
      setWorktreeError("")
      setWorktreeTarget({ id: row.worktreeId, path: row.path, project })
    }
    const observer = new MutationObserver(decorate)
    observer.observe(lifecycle, { childList: true, subtree: true })
    lifecycle.addEventListener("click", onRemove)
    decorate()
    return () => { observer.disconnect(); lifecycle.removeEventListener("click", onRemove) }
  }, [])

  useLayoutEffect(() => {
    const rows = document.getElementById("projects-rows")
    const status = document.getElementById("projects-status")
    if (!rows || !status) return
    const decorate = () => {
      for (const button of rows.querySelectorAll<HTMLButtonElement>("button.project-row[data-place-id]")) {
        const place = page.current?.state.places?.find((candidate) => candidate.id === button.dataset.placeId)
        if (!place) continue
        if (!Number.isSafeInteger(place.activeItemCount) || (place.activeItemCount ?? -1) < 0) {
          button.querySelector(".project-row-activity")?.remove()
        }
        const name = place.label || place.path || ""
        const label = projectOpenLabel(name, null, false)
        if (button.getAttribute("aria-label") !== label) button.setAttribute("aria-label", label)
        if (catalogWordLanguage("legacy", "webProjectOpenLabel") === "en" && currentCatalogTag() !== "en") button.lang = "en"
      }
      for (const project of rows.querySelectorAll<HTMLButtonElement>(".project-row[data-place-id]")) {
        const wrapper = project.closest<HTMLElement>(".project-row-wrap")
        const worktrees = wrapper?.querySelector<HTMLButtonElement>(".project-row-worktrees")
        if (worktrees) {
          const word = catalogWord("next", "projectsWorktrees")
          const name = wrapper?.querySelector(".project-row-name")?.textContent || ""
          worktrees.title = word
          worktrees.setAttribute("aria-label", `${word}: ${name}`)
          if (catalogWordLanguage("next", "projectsWorktrees") === "en" && currentCatalogTag() !== "en") worktrees.lang = "en"
        }
        if (!wrapper || !project || wrapper.querySelector(".project-row-settings")) continue
        const gear = document.createElement("button")
        gear.type = "button"
        gear.className = "project-row-settings"
        gear.dataset.placeId = project.dataset.placeId
        gear.title = catalogWord("literal", "00186c9a0293")
        gear.setAttribute("aria-label", catalogFormat("template", "78b00d86df37", [wrapper.querySelector(".project-row-name")?.textContent || catalogWord("literal", "fb67023ade0c")]))
        gear.setAttribute("aria-haspopup", "dialog")
        gear.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <path d="M10.09 4.54h3.82l.54 1.66 1.35.77 1.71-.35 1.9 3.3-1.16 1.3v1.56l1.16 1.3-1.9 3.3-1.71-.35-1.35.77-.54 1.66h-3.82l-.54-1.66-1.35-.77-1.71.35-1.9-3.3 1.16-1.3v-1.56l-1.16-1.3 1.9-3.3 1.71.35 1.35-.77Z"/>
          <circle cx="12" cy="12" r="2.5"/>
        </svg>`
        wrapper.classList.add("has-settings")
        wrapper.appendChild(gear)
        // 終端: this project's terminals, locally or through the selected Cloud machine.
        const name = wrapper.querySelector(".project-row-name")?.textContent || nextWord("terminalEntry")
        const terminal = document.createElement("button")
        terminal.type = "button"
        terminal.className = "project-row-terminal"
        terminal.dataset.placeId = project.dataset.placeId
        terminal.title = nextWord("terminalEntry")
        terminal.setAttribute("aria-label", nextWord("terminalEntryFor", { project: name }))
        terminal.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="m7.5 10 3 2.5-3 2.5M12.5 15h4"/>
        </svg>`
        wrapper.classList.add("has-terminal")
        wrapper.appendChild(terminal)
        const remove = document.createElement("button")
        remove.type = "button"
        remove.className = "project-row-remove"
        remove.dataset.placeId = project.dataset.placeId
        remove.title = nextWord("projectsRemove")
        remove.setAttribute("aria-label", nextWord("projectsRemoveFor", { project: name }))
        remove.setAttribute("aria-haspopup", "dialog")
        remove.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M4 7h16M9 7V4h6v3m3 0-1 13H7L6 7m4 4v5m4-5v5"/></svg>`
        wrapper.classList.add("has-remove")
        wrapper.appendChild(remove)
      }
      const localizedStatus = localizePinnedProjectStatus(status.textContent ?? "")
      if (localizedStatus !== status.textContent) status.textContent = localizedStatus
      showProjectRefusalDetail(status, latestProjectReadRefusal())
    }
    const onSettings = (ev: Event) => {
      const gear = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-settings[data-place-id]")
      if (!gear || !rows.contains(gear)) return
      const project = page.current?.state.places?.find(place => place.id === gear.dataset.placeId)
      if (project?.path) setup.current?.open(project.path, gear)
    }
    const onTerminal = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-terminal[data-place-id]")
      if (!button || !rows.contains(button)) return
      // Every project's terminals are in the Session page's terminal list,
      // each row named by its project; there is no per-project page any more.
      const project = page.current?.state.places?.find((place) => place.id === button.dataset.placeId)
      if (project?.path) openTerminalPage(project.path)
    }
    const onRemove = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-remove[data-place-id]")
      if (!button || !rows.contains(button)) return
      const project = page.current?.state.places?.find(place => place.id === button.dataset.placeId)
      if (project?.path) { setRemoveError(""); setRemoveTarget(project) }
    }
    rows.addEventListener("click", onSettings)
    rows.addEventListener("click", onTerminal)
    rows.addEventListener("click", onRemove)
    const observer = new MutationObserver(decorate)
    observer.observe(rows, { childList: true, subtree: true })
    observer.observe(status, { childList: true })
    decorate()
    return () => {
      observer.disconnect()
      rows.removeEventListener("click", onSettings)
      rows.removeEventListener("click", onTerminal)
      rows.removeEventListener("click", onRemove)
    }
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    // The language arrives in App's effect, which runs after this one on a cold
    // start at `#page=projects`; the page is still covered (`booting`) until
    // then, so arrival waits for them as the original's does.
    const arrive = () => {
      if (!was.current) return
      // Static copy is painted once; the Board answer may rewrite the lede after.
      if (!painted.current) {
        painted.current = true
        paintNextWords(document.getElementById("projects"))
      }
      const add = document.getElementById("projects-add")
      if (add) add.textContent = catalogWord("projects", "addButton")
      // An address that names a Project (a reload, a link) opens it once the
      // list is read, with the list put under it as one Back step.
      const wanted = detailInHash("projects", location.hash)
      if (wanted) arriveAtDetail("projects", wanted)
      void page.current?.enter(wanted)
      document.getElementById("projects-title")?.focus({ preventScroll: true })
    }
    const root = document.documentElement
    if (!root.classList.contains("booting")) {
      arrive()
      return
    }
    const watch = new MutationObserver(() => {
      if (root.classList.contains("booting")) return
      watch.disconnect()
      arrive()
    })
    watch.observe(root, { attributes: true, attributeFilter: ["class"] })
    return () => watch.disconnect()
  }, [shown])

  // Back, Forward and a typed address move the open Project the way the
  // page's own back control does.
  useLayoutEffect(() => {
    if (!shown) return
    const follow = () => {
      if (!/(?:^|[#&])page=projects(?:&|$)/.test(location.hash)) return
      void page.current?.follow(detailInHash("projects", location.hash))
    }
    window.addEventListener("popstate", follow)
    window.addEventListener("hashchange", follow)
    return () => {
      window.removeEventListener("popstate", follow)
      window.removeEventListener("hashchange", follow)
    }
  }, [shown])

  useLayoutEffect(() => {
    if (!shown) return
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape" || ev.metaKey || ev.ctrlKey) return
      if (page.current?.state.view !== "detail") return
      if (ActionConfirm.isOpen() || Info.isOpen() || overlayShown("keys")) return
      if (!(document.getElementById("sidebar")?.hidden ?? true)) return
      if (document.querySelector("dialog[open]")) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      page.current?.escape()
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [shown])

  return (
    <>
    <section
      className="page projects"
      id="projects"
      data-page-view="projects"
      aria-labelledby="projects-title"
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
    {toolsHost && createPortal(<>
      <ProjectTools shown={shown} changed={() => { void page.current?.enter() }} setupRef={setup} />
    </>, toolsHost)}
    <ProjectFolderPicker open={addOpen} onClose={() => setAddOpen(false)} onAdded={() => { void page.current?.enter() }} />
    <dialog className="project-remove-dialog" ref={removeDialog}
      onCancel={(event) => { if (removeBusy) event.preventDefault() }}
      onClose={() => { if (!removeBusy) setRemoveTarget(null) }}>
      <h2>{nextWord("projectsRemoveTitle")}</h2>
      <p>{nextWord("projectsRemoveDescription", { project: removeTarget?.label || removeTarget?.path || "" })}</p>
      {removeError && <p className="project-remove-error" role="alert">{removeError}</p>}
      <div className="project-remove-actions">
        <button type="button" disabled={removeBusy} onClick={() => setRemoveTarget(null)}>{nextWord("projectsRemoveCancel")}</button>
        <button type="button" disabled={removeBusy} onClick={() => void confirmRemove()}>{removeBusy ? nextWord("projectsRemoveBusy") : nextWord("projectsRemoveConfirm")}</button>
      </div>
    </dialog>
    <dialog className="project-remove-dialog" ref={worktreeDialog}
      onCancel={(event) => { if (worktreeBusy) event.preventDefault() }}
      onClose={() => { if (!worktreeBusy) setWorktreeTarget(null) }}>
      <h2>{nextWord("worktreeRemoveTitle")}</h2>
      <p>{nextWord("worktreeRemoveDescription", { path: worktreeTarget?.path || "" })}</p>
      {worktreeError && <p className="project-remove-error" role="alert">{worktreeError}</p>}
      <div className="project-remove-actions">
        <button type="button" disabled={worktreeBusy} onClick={() => setWorktreeTarget(null)}>{nextWord("projectsRemoveCancel")}</button>
        <button type="button" disabled={worktreeBusy} onClick={() => void confirmWorktreeRemove()}>{worktreeBusy ? nextWord("worktreeRemoveBusy") : nextWord("worktreeRemoveConfirm")}</button>
      </div>
    </dialog>
    </>
  )
}

/** The page router, as the drawer moves: its row for that page, clicked. */
function navigate(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) row.click()
}

export const page: PageModule = { id: "projects", Component: ProjectsPageView }

function paintNextWords(root: ParentNode | null): void {
  if (!root) return
  for (const node of root.querySelectorAll<HTMLElement>("[data-next-word]")) {
    const key = node.dataset.nextWord as NextWord | undefined
    if (!key) continue
    const value = nextWord(key)
    if (node.textContent !== value) node.textContent = value
  }
}

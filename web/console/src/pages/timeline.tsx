import { catalogWord } from "../catalog.js"
import { useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import { bindTimeline, requestedTimeline, timelineReturn, type TimelinePage } from "../legacy/timeline-bridge.js"
import { machineWording } from "../legacy/machine-copy.js"
import { ActionConfirm, Info, shown as overlayShown } from "../overlays/index.js"
import { arriveAtDetail, detailInHash, enterDetail, leaveDetail } from "../page-detail.js"
import sectionMarkup from "./timeline/section.html?raw"
import "../page-bar.css"

/** True when the history step under the reader is this page's open entry. */
function ownEntryStep(): boolean {
  const state = history.state as { view?: unknown; page?: unknown } | null
  return state?.view === "page-detail" && state.page === "timeline"
}

/**
 * The Project Timeline: `section#timeline` in the Swift app's `index.html`,
 * drawn by its `view/timeline.js`.
 *
 * The fragment beside this file is that section's markup between its own tags,
 * whitespace included, and the copied module fills it — the date groups, the
 * cards, the work-item pills, the evidence list and the filters are the
 * original's own DOM.
 *
 * It is not a drawer row, there or here: a timeline is one Project's, so it is
 * reached from something that already has a Project, which is what hands it
 * the Project through `openTimeline`.
 *
 * **Where it is reached from changed** (work-system-review §5.2, W6). The old
 * Project Board was a frozen snapshot presented as a live page. It has been
 * removed. The v1 work page once opened this timeline from its Board view;
 * that page was retired when v2 became the Board reader.
 *
 * `boardItemIds` belong to the old store and have no proved mapping to work
 * ids. The copied renderer still emits their pills, so this host removes them
 * as they are drawn instead of turning an unknown relation into a link.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter(project)` on arrival, `leave` on departure, and the keyboard lands on
 * the heading. Escape is the module's own: an open entry gives itself back, and
 * the page gives itself back to the page that opened it.
 */
function TimelinePageView({ shown }: { shown: boolean }) {
  const page = useRef<TimelinePage | null>(null)
  const was = useRef(false)

  useLayoutEffect(() => {
    const root = document.getElementById("timeline")
    root?.querySelector("#timeline-board-tab")?.remove()
    const discardUnmappedPills = () => {
      for (const pill of root?.querySelectorAll(".timeline-board-pill, .timeline-board-more") ?? []) pill.remove()
      const status = root?.querySelector("#timeline-status")
      if (status?.textContent) {
        const wording = machineWording(status.textContent, document.documentElement.lang)
        if (wording !== status.textContent) status.textContent = wording
      }
    }
    discardUnmappedPills()
    const observer = root ? new MutationObserver(discardUnmappedPills) : null
    if (root && observer) observer.observe(root, { childList: true, subtree: true })
    if (!page.current) {
      // In the copied module this callback means "back to Board". With that
      // page retired it returns to the page that supplied the Project. Pill
      // calls never survive the observer above.
      page.current = bindTimeline(document, () => navigate(timelineReturn()))
    }
    return () => observer?.disconnect()
  }, [])

  // An open entry is a history step (`#page=timeline&id=<entry>`, page-detail.ts):
  // a card opens it, and both ways back — the module's own two buttons and the
  // browser's Back — go back over that step, so they agree. The page's Read
  // again is an icon on the right; it presses the module's own hidden button,
  // whose word it carries as its name.
  const listScroll = useRef(0)
  useLayoutEffect(() => {
    const root = document.getElementById("timeline")
    const reload = document.getElementById("timeline-reload")
    const moduleRefresh = document.getElementById("timeline-refresh")
    if (!root || !reload || !moduleRefresh) return
    const name = () => {
      const word = moduleRefresh.textContent?.trim() || "Refresh"
      reload.setAttribute("aria-label", word)
      reload.title = word
    }
    name()
    const words = new MutationObserver(name)
    words.observe(moduleRefresh, { childList: true, characterData: true, subtree: true })
    const onReload = () => moduleRefresh.click()
    const restoreList = () => requestAnimationFrame(() => { root.scrollTop = listScroll.current })
    const onClick = (ev: MouseEvent) => {
      const target = ev.target as Element | null
      const card = target?.closest?.<HTMLElement>(".timeline-card")
      if (card?.dataset.timelineEntry) {
        listScroll.current = root.scrollTop
        root.scrollTop = 0
        enterDetail("timeline", card.dataset.timelineEntry)
        return
      }
      const back = target?.closest?.(".timeline-detail-back, #timeline-back")
      if (!back || !page.current?.state.entryId) return
      if (ownEntryStep()) {
        // Back over the step; popstate closes the entry.
        ev.stopImmediatePropagation()
        ev.preventDefault()
        history.back()
        return
      }
      leaveDetail("timeline")
      restoreList()
    }
    const onPop = () => {
      if (root.hidden) return
      const bound = page.current
      if (!bound) return
      const wanted = detailInHash("timeline", location.hash) || null
      if (wanted === bound.state.entryId) return
      if (wanted) {
        bound.state.entryId = wanted
        void bound.refresh()
      } else {
        bound.leave()
        restoreList()
      }
    }
    root.addEventListener("click", onClick, true)
    reload.addEventListener("click", onReload)
    window.addEventListener("popstate", onPop)
    return () => {
      words.disconnect()
      root.removeEventListener("click", onClick, true)
      reload.removeEventListener("click", onReload)
      window.removeEventListener("popstate", onPop)
    }
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    const arrive = () => {
      if (!was.current) return
      const project = requestedTimeline()
      // A timeline with no Project is not a page, it is a question nobody
      // asked. The Projects page is where the Project is chosen, so that is
      // where a reader who arrived here by address is sent.
      if (!project) {
        navigate(timelineReturn())
        return
      }
      // An address naming an entry opens it with the Project (a reload keeps
      // the address but not the Project, so that one is sent back above).
      const entry = detailInHash("timeline", location.hash)
      if (entry && page.current) {
        page.current.state.entryId = entry
        arriveAtDetail("timeline", entry)
      }
      void page.current?.enter(project)
      document.getElementById("timeline-title")?.focus({ preventScroll: true })
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

  useLayoutEffect(() => {
    if (!shown) return
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape" || ev.metaKey || ev.ctrlKey) return
      if (ActionConfirm.isOpen() || Info.isOpen() || overlayShown("keys")) return
      if (!(document.getElementById("sidebar")?.hidden ?? true)) return
      if (document.querySelector("dialog[open]")) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      if (page.current?.state.entryId && ownEntryStep()) history.back()
      else {
        if (page.current?.state.entryId) leaveDetail("timeline")
        page.current?.escape()
      }
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [shown])

  return (
    <section
      className="page timeline-page"
      id="timeline"
      data-page-view="timeline"
      aria-label={catalogWord("inline", "da2fd084302a")}
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/** The page router, as the drawer moves: its row for that page, clicked. */
function navigate(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) row.click()
}

export const page: PageModule = { id: "timeline", Component: TimelinePageView }

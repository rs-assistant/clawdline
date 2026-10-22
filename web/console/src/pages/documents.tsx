import { useEffect, useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import {
  OPEN_DOCUMENTS,
  bindDocuments,
  documentIdentityForSession,
  documentLocatorFromHash,
  hasDocumentIntent,
  publishedRows,
  type DocumentsPage,
  type DocumentsRequest,
} from "../legacy/documents-bridge.js"
import { requestPage } from "../overlays/index.js"
import * as L from "../legacy/bridge.js"
import { documentHash } from "../legacy/js/net/document-links.js"
import { leaveDetail, listHash } from "../page-detail.js"
import { bindDocumentFullscreen, type DocumentFullscreen } from "./documents/fullscreen.js"
import sectionMarkup from "./documents/section.html?raw"
import "../page-bar.css"
import "./documents/page.css"

/**
 * An open document is a history step whose address is the document's own link
 * (`#document=1&machine=…`), so the browser's Back returns to the listing and a
 * reload reopens the document. The step is marked as page-detail.ts marks its
 * own, so `leaveDetail` goes back over it and over nothing else.
 */
const STEP = { view: "page-detail", page: "documents" }

function ownStep(): boolean {
  const state = history.state as { view?: unknown; page?: unknown } | null
  return state?.view === STEP.view && state.page === STEP.page
}

/**
 * Open the document's step: replace an open document's step, or put the list's
 * address under it first. The entry under it is the page's own list address, not
 * the Session's this page was entered from without an address, so Back does not
 * leave the page.
 */
function stepToDocument(address: string): void {
  if (location.hash === address) return
  try {
    if (ownStep()) history.replaceState({ ...STEP, id: address }, "", address)
    else {
      history.replaceState(history.state, "", listHash("documents"))
      history.pushState({ ...STEP, id: address }, "", address)
    }
  } catch {
    /* the address stays as it was */
  }
}

/**
 * The Documents page: `section#documents-page` in the Swift app's `index.html`,
 * driven by its `view/documents.js`.
 *
 * The fragment beside this file is that section's markup between its own tags
 * (index.html lines 551–574), whitespace included, and the copied module fills
 * it: its words, the listing, one document, the two share controls and the
 * status line. React owns the section element and nothing inside it.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter` on arrival and `leave` on departure, the keyboard landing on the
 * heading; and `#session-documents` in the `⋯` menu resolves the row's identity
 * with `documentIdentityForSession` and opens the page on it, where a refusal —
 * two Macs publishing one id, a row with no identity — is drawn by
 * `openSessionError` rather than swallowed.
 *
 * A complete `#document=…` address is opened here after `App` selects this
 * page. The locator is validated before the copied document controller reads
 * it; malformed links show its error state instead of becoming a list route.
 *
 * `transportChanged` is not wired either: it exists there for a Cloud
 * connection that comes back, and this page has one transport that is either
 * answering or not. The module's own retry — `held` and `scheduleRetry` — is
 * still what handles a refusal it can wait out.
 */
function DocumentsPageView({ shown }: { shown: boolean }) {
  const page = useRef<DocumentsPage | null>(null)
  const fullscreen = useRef<DocumentFullscreen | null>(null)
  const was = useRef(false)

  useLayoutEffect(() => {
    if (!page.current) page.current = bindDocuments(document, navigate)
    if (!fullscreen.current) fullscreen.current = bindDocumentFullscreen(document)
  }, [])

  // A row opened, the way back to the listing, Read again, and the browser's
  // Back: the address and the module agree after each. `fromPop` keeps a Back
  // that already happened from going Back a second time through the button.
  const listScroll = useRef(0)
  useEffect(() => {
    const root = document.getElementById("documents-page")
    const listBack = document.getElementById("document-list-back")
    const refresh = document.getElementById("documents-refresh")
    if (!root || !listBack || !refresh) return
    let fromPop = false
    const onClick = (ev: MouseEvent) => {
      if (!(ev.target as Element | null)?.closest?.(".document-row")) return
      const locator = page.current?.state().locator
      if (!locator) return
      listScroll.current = root.scrollTop
      root.scrollTop = 0
      try { stepToDocument(documentHash(locator)) } catch { /* a locator the link cannot carry */ }
    }
    const onListBack = (ev: MouseEvent) => {
      if (fromPop) return
      if (ownStep()) {
        // Back over the step; the popstate below closes the document.
        ev.stopImmediatePropagation()
        history.back()
        return
      }
      leaveDetail("documents")
      restoreList()
    }
    const restoreList = () => requestAnimationFrame(() => { root.scrollTop = listScroll.current })
    const onPop = () => {
      if (root.hidden || hasDocumentIntent(location.hash)) return
      if (!page.current?.state().locator) return
      fromPop = true
      try { listBack.click() } finally { fromPop = false }
      restoreList()
    }
    const onRefresh = () => { void page.current?.enter() }
    root.addEventListener("click", onClick)
    listBack.addEventListener("click", onListBack, true)
    refresh.addEventListener("click", onRefresh)
    window.addEventListener("popstate", onPop)
    return () => {
      root.removeEventListener("click", onClick)
      listBack.removeEventListener("click", onListBack, true)
      refresh.removeEventListener("click", onRefresh)
      window.removeEventListener("popstate", onPop)
    }
  }, [])

  // `byId("session-documents").addEventListener` in `main.js`, moved to an
  // event because the menu row is drawn by a component that does not own this
  // page. The menu closes itself before this runs, as `SessionActions.close()`
  // does there.
  useEffect(() => {
    const open = (ev: Event) => {
      const bound = page.current
      const id = (ev as CustomEvent<DocumentsRequest>).detail?.id
      if (!bound || !id) return
      let identity
      try {
        identity = documentIdentityForSession(publishedRows(), id)
      } catch (error) {
        bound.openSessionError(error)
        return
      }
      bound.openSession(identity)
    }
    document.addEventListener(OPEN_DOCUMENTS, open)
    return () => document.removeEventListener(OPEN_DOCUMENTS, open)
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      fullscreen.current?.close()
      page.current?.leave()
      return
    }
    // The words arrive in App's effect, which runs after this one on a cold
    // start at `#page=documents`; the page is still covered (`booting`) until
    // then, and `enter` paints its heading from them, so arrival waits.
    const arrive = () => {
      if (!was.current) return
      const refresh = document.getElementById("documents-refresh")
      if (refresh) {
        refresh.setAttribute("aria-label", L.strings.webInfoRefresh)
        refresh.title = L.strings.webInfoRefresh
      }
      // A link named the document. Its step is the link's own: Back from it is
      // wherever the link was followed from (a Session's note), not this list.
      if (hasDocumentIntent(location.hash)) page.current?.openDirect(documentLocatorFromHash(location.hash))
      void page.current?.enter()
      document.getElementById("documents-title")?.focus({ preventScroll: true })
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

  useEffect(() => {
    if (!shown) return
    const followDocument = () => {
      if (hasDocumentIntent(location.hash)) page.current?.openDirect(documentLocatorFromHash(location.hash))
    }
    window.addEventListener("hashchange", followDocument)
    return () => window.removeEventListener("hashchange", followDocument)
  }, [shown])

  return (
    <section
      className="page documents-page"
      id="documents-page"
      data-page-view="documents"
      aria-labelledby="documents-title"
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/**
 * The page router: `main.js`'s own `navigate` for this module, which for this
 * page is `Pages.go(name, { hash: false })` — the page moves and the address is
 * not written — and for any other page writes that page's address.
 *
 * ```js
 * navigate: function (name, options) {
 *     return Pages.go(name, name === "documents" ? { hash: false } : options);
 * }
 * ```
 *
 * Documents has no drawer row — it has none in the original either, where it is
 * reached from the session menu and from a document address. This used to reach
 * `App`'s router the other way round, by writing `#page=documents` and letting
 * the `hashchange` arrive; on a phone that never worked, because the write is a
 * same-document navigation, the `popstate` it fires is the back gesture, and
 * the detail closing behind it replaced the address before anybody read it. So
 * the page is asked for directly, as it is there.
 */
function navigate(name: string): void {
  // Leaving for another page writes that page's address, as `main.js` did for
  // every page but this one; the list's own address (`#page=documents`, put
  // under an open document) must not outlive the page it names.
  requestPage(name === "documents" ? { page: name, hash: false } : { page: name })
}

export const page: PageModule = { id: "documents", Component: DocumentsPageView }

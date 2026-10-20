/**
 * Reading one document full screen.
 *
 * The viewer itself becomes a layer over the whole window (`is-fullscreen`),
 * so the same thing happens on every browser. Where the browser can also take
 * the element full screen (desktop browsers, Android), it is asked to, and its
 * own way out — Esc — leaves this mode too. iPhone Safari cannot take an
 * element full screen, so there the layer is the whole of it.
 *
 * The heading stays the controls: after a few seconds it folds into one round
 * button in the corner, and pressing that button brings it back. It folds on a
 * timer because the document is in a sandboxed frame this page cannot see into:
 * neither its scrolling nor a tap on it reaches here.
 *
 * Entering is a history step at the same address, so the phone's back gesture
 * and the browser's Back leave full screen before they leave the document.
 */

export const FOLD_AFTER_MS = 3000

type Step = { documentFullscreen?: boolean } | null

export interface DocumentFullscreen {
  active(): boolean
  /** Leave full screen without stepping history; the caller already moved it. */
  close(): void
  dispose(): void
}

export function bindDocumentFullscreen(doc: Document): DocumentFullscreen | null {
  const byId = (id: string) => doc.getElementById(id)
  const viewer = byId("document-viewer")
  const heading = viewer?.querySelector<HTMLElement>(".document-heading") ?? null
  const toggle = byId("document-fullscreen") as HTMLButtonElement | null
  const reveal = byId("document-reveal") as HTMLButtonElement | null
  const page = byId("documents-page")
  if (!viewer || !heading || !toggle || !reveal) return null
  const win = doc.defaultView ?? window
  let on = false
  let pointerInside = false
  let timer: ReturnType<typeof setTimeout> | null = null

  const ownStep = () => !!(win.history.state as Step)?.documentFullscreen

  const unfold = () => {
    viewer.classList.remove("chrome-folded")
    reveal.hidden = true
    schedule()
  }
  const fold = () => {
    timer = null
    if (!on) return
    // Someone working the controls keeps them: a pointer over the bar, or a
    // keyboard focus inside it.
    const focused = doc.activeElement
    if (pointerInside || (focused && heading.contains(focused) && focused.matches(":focus-visible"))) {
      schedule()
      return
    }
    viewer.classList.add("chrome-folded")
    reveal.hidden = false
  }
  function schedule() {
    if (timer !== null) clearTimeout(timer)
    timer = setTimeout(fold, FOLD_AFTER_MS)
  }

  const enter = () => {
    if (on || toggle.disabled) return
    on = true
    viewer.classList.add("is-fullscreen")
    page?.classList.add("is-fullscreen")
    try {
      win.history.pushState({ ...(win.history.state ?? {}), documentFullscreen: true }, "", win.location.href)
    } catch { /* full screen still opens; Back then leaves the document */ }
    const request = viewer.requestFullscreen as undefined | (() => Promise<void>)
    // A request can still be pending when the person has already left (two
    // quick presses): the browser then takes the window full screen after
    // `close` looked and found nothing to leave. Leave it when it lands.
    if (typeof request === "function") request.call(viewer).then(release, () => { /* the layer is enough */ })
    unfold()
  }

  function release() {
    if (!on && doc.fullscreenElement === viewer) doc.exitFullscreen().catch(() => { /* already out */ })
  }

  const close = () => {
    if (!on) return
    on = false
    if (timer !== null) clearTimeout(timer)
    timer = null
    viewer.classList.remove("is-fullscreen", "chrome-folded")
    page?.classList.remove("is-fullscreen")
    reveal.hidden = true
    release()
    if (!viewer.hidden) toggle.focus({ preventScroll: true })
  }

  /** The way out a person takes: back over the step when it is ours. */
  const leave = () => {
    if (!on) return
    if (ownStep()) win.history.back()
    else close()
  }

  const onToggle = () => { if (on) leave(); else enter() }
  const onReveal = () => {
    unfold()
    toggle.focus({ preventScroll: true })
  }
  const onPop = () => { if (on && !ownStep()) close() }
  const onBrowserFullscreen = () => {
    // Esc in the browser's full screen: leave ours as well.
    if (on && doc.fullscreenElement !== viewer) leave()
    else release()
  }
  const onKey = (ev: KeyboardEvent) => {
    if (on && ev.key === "Escape" && !doc.fullscreenElement) {
      ev.preventDefault()
      leave()
    }
  }
  const onEnter = () => { pointerInside = true }
  const onLeave = () => { pointerInside = false }
  // The document closed under us (its way back, another document's address).
  const watch = new MutationObserver(() => { if (on && viewer.hidden) close() })

  toggle.addEventListener("click", onToggle)
  reveal.addEventListener("click", onReveal)
  heading.addEventListener("pointerenter", onEnter)
  heading.addEventListener("pointerleave", onLeave)
  viewer.addEventListener("keydown", onKey)
  win.addEventListener("popstate", onPop)
  doc.addEventListener("fullscreenchange", onBrowserFullscreen)
  watch.observe(viewer, { attributes: true, attributeFilter: ["hidden"] })

  return {
    active: () => on,
    close,
    dispose: () => {
      close()
      watch.disconnect()
      toggle.removeEventListener("click", onToggle)
      reveal.removeEventListener("click", onReveal)
      heading.removeEventListener("pointerenter", onEnter)
      heading.removeEventListener("pointerleave", onLeave)
      viewer.removeEventListener("keydown", onKey)
      win.removeEventListener("popstate", onPop)
      doc.removeEventListener("fullscreenchange", onBrowserFullscreen)
    },
  }
}

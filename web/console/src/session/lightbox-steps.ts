// Previous and next in the picture preview.
//
// The preview itself (`legacy/js/view/transcript-images.js`) is the Swift
// app's module, pinned byte for byte, and knows only the one picture it was
// opened on. Stepping is therefore done from outside it, the one way that
// module already accepts a picture: the neighbouring tile is pressed, and its
// own click opens the preview on its own bytes — the same source, the same
// expiry callback and the same focus return as a press by the reader.
//
// Neighbours are the transcript's live tiles in page order, read at every
// step rather than kept, so a picture that finished loading after the preview
// opened is reachable and one that expired is skipped.

/** The three things this adds to the preview, in the reader's language. */
export interface LightboxStepWords {
  previous: string
  next: string
  position: (index: number, count: number) => string
}

/** What `attachLightboxSteps` needs from a tile and its surroundings. */
interface Tile {
  click(): void
  closest(selector: string): Element | null
}

// A tile the reader can open: `connectArtifactTile` sets this state when the
// bytes have arrived, and only then does its click open the preview.
const LIVE_TILE = '.message-image-tile[data-image-state="live"]'
// The transcript a tile belongs to. Two transcripts can be on screen (the
// fleet view), and stepping out of one into another would be a surprise.
const TRANSCRIPT = ".tx"
// A swipe is a horizontal travel at least this long, and at least twice as
// long as its vertical one, so a reader scrolling a long screenshot never
// changes pictures by accident.
const SWIPE_MIN = 56

/** The live pictures around `current`, in reading order. */
export function livePictures(current: Tile, doc: ParentNode): Element[] {
  const scope: ParentNode = (current.closest(TRANSCRIPT) as ParentNode | null) ?? doc
  return Array.from(scope.querySelectorAll(LIVE_TILE))
}

/** The tile `step` places from `current`, or null at either end. */
export function stepFrom<T>(list: readonly T[], current: T, step: number): T | null {
  const at = list.indexOf(current)
  if (at < 0) return null
  return list[at + step] ?? null
}

/** Whether a one-finger travel was a swipe, and which way: -1 back, 1 on, 0 neither. */
export function swipeStep(dx: number, dy: number): -1 | 0 | 1 {
  if (Math.abs(dx) < SWIPE_MIN || Math.abs(dx) < 2 * Math.abs(dy)) return 0
  // A finger moving left brings the next picture in from the right.
  return dx < 0 ? 1 : -1
}

/**
 * Put the two buttons and the count in the preview and answer the arrow keys
 * and a horizontal swipe. Call `opened` every time the preview opens on a tile.
 */
export function attachLightboxSteps(
  dialog: HTMLElement,
  image: HTMLElement,
  words: LightboxStepWords,
  doc: Document = document,
): { opened: (trigger: Element) => void } {
  const frame = dialog.querySelector<HTMLElement>(".image-lightbox-frame") ?? dialog
  let current: (Tile & Element) | null = null

  const button = (step: -1 | 1, label: string, glyph: string): HTMLButtonElement => {
    const b = doc.createElement("button")
    b.type = "button"
    b.className = "image-lightbox-step"
    b.dataset.step = String(step)
    b.setAttribute("aria-label", label)
    b.title = label
    b.textContent = glyph
    b.hidden = true
    b.addEventListener("click", () => go(step))
    // The preview's own handlers read every touch and press inside it: two
    // quick taps here would be a double tap and zoom the picture instead of
    // moving on twice. A press on these buttons is only a press.
    for (const kind of ["touchstart", "touchend", "pointerdown"]) {
      b.addEventListener(kind, (event) => event.stopPropagation())
    }
    frame.appendChild(b)
    return b
  }
  const back = button(-1, words.previous, "‹")
  const on = button(1, words.next, "›")
  const count = doc.createElement("span")
  count.className = "image-lightbox-count"
  count.hidden = true
  frame.appendChild(count)

  function show(): void {
    const list = current ? livePictures(current, doc) : []
    const at = current ? list.indexOf(current) : -1
    const many = at >= 0 && list.length > 1
    back.hidden = !many || at === 0
    on.hidden = !many || at === list.length - 1
    count.hidden = !many
    count.textContent = many ? words.position(at + 1, list.length) : ""
  }

  function go(step: number): boolean {
    if (dialog.hidden || !current) return false
    const target = stepFrom(livePictures(current, doc), current, step) as (Tile & Element) | null
    if (!target) return false
    target.click()
    return true
  }

  doc.addEventListener(
    "keydown",
    (event) => {
      if (dialog.hidden || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
      const step = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0
      if (!step) return
      // Taken even at either end: an arrow that reaches the page behind the
      // preview scrolls or selects something the reader cannot see.
      event.preventDefault()
      event.stopPropagation()
      go(step)
    },
    true,
  )

  let swipe: { x: number; y: number } | null = null
  dialog.addEventListener("touchstart", (event) => {
    // An enlarged picture is dragged by one finger, so the swipe belongs to it.
    const t = event.touches
    swipe = t.length === 1 && image.dataset.zoomed !== "true" ? { x: t[0].clientX, y: t[0].clientY } : null
  })
  dialog.addEventListener("touchend", (event) => {
    const start = swipe
    swipe = null
    const end = event.changedTouches[0]
    if (!start || !end || event.touches.length) return
    const step = swipeStep(end.clientX - start.x, end.clientY - start.y)
    if (step) go(step)
  })
  dialog.addEventListener("touchcancel", () => {
    swipe = null
  })

  return {
    opened(trigger: Element) {
      current = trigger as Tile & Element
      show()
    },
  }
}

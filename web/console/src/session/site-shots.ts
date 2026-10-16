// The website's feature pictures: one small story, written separately in each
// language, and the callouts drawn over it. `site-shots.e2e.ts` serves the
// story to the built console and photographs it; `tools/site-shots.sh` sets
// SITE_SHOTS to an output directory and SITE_SHOTS_LANG to `en` or `zh-Hant`.
// docs/site-shots.md says how the pictures are used.

export const SITE_SHOTS = process.env.SITE_SHOTS || ""
export const SITE_LANG = process.env.SITE_SHOTS_LANG === "en" ? "en" : "zh-Hant"
export const siteSuffix = SITE_LANG

/**
 * Someone has an Agent rebuilding their shop's checkout. It asks whether to go
 * live, they queue the next fix with a screenshot, and the Board shows where
 * each piece of work stands. Each language tells it in its own words rather
 * than through a translation table, so neither picture reads as translated.
 */
const STORIES = {
  en: {
    project: "Shop",
    checkout: "Checkout",
    signIn: "Google sign-in",
    search: "Search",
    asked: "Turn checkout into a single page.",
    answered: "Done: shipping, payment and review now sit on one page. All 42 tests pass.",
    note: {
      title: "Ship the new checkout now?",
      summary: "The one-page checkout is done and every test passes.",
      action: "Choose how to release it.",
      reason: "Real customers will see it, so it is your call.",
      options: [
        { label: "Staging first", draft: "Put it on staging first; I'll look before it goes live." },
        { label: "Go live", draft: "Go live now." },
      ],
    },
    todo: "The Pay button is cut off on iPhone. Fix it after this.",
    screenshot: "iphone-checkout.png",
    items: {
      checkout: ["One-page checkout", "Shipping, payment and review on a single page."],
      signIn: ["Sign in with Google", "Returning customers skip the password."],
      search: ["Search that forgives typos", "\"sneekers\" still finds sneakers."],
      reviews: ["Photos in product reviews", "Customers can attach up to three photos."],
    },
  },
  "zh-Hant": {
    project: "網路商店",
    checkout: "結帳頁改版",
    signIn: "Google 登入",
    search: "商品搜尋",
    asked: "把結帳流程改成單頁。",
    answered: "完成：寄送、付款、確認都在同一頁了，42 個測試全部通過。",
    note: {
      title: "新結帳頁要直接上線嗎？",
      summary: "單頁式結帳做好了，測試全部通過。",
      action: "選擇上線方式。",
      reason: "真實顧客會看到，要由你決定。",
      options: [
        { label: "先上測試站", draft: "先放到測試站，我看過再上線。" },
        { label: "直接上線", draft: "直接上線。" },
      ],
    },
    todo: "iPhone 上的「付款」按鈕被切掉了，這個做完後修一下。",
    screenshot: "iphone-結帳頁.png",
    items: {
      checkout: ["單頁式結帳", "寄送、付款、確認放在同一頁。"],
      signIn: ["用 Google 登入", "老顧客不用再輸入密碼。"],
      search: ["搜尋容許錯字", "打「布鞋子」也找得到布鞋。"],
      reviews: ["評價可以附照片", "顧客最多可附三張照片。"],
    },
  },
} as const

export const STORY = STORIES[SITE_LANG]

export type Callout = { sel: string; n: number; label: string; pad?: number }

/**
 * A page expression that marks the real elements' boxes on the live page: a
 * hairline outline with a small number at its corner, and a light shade over
 * the rest. The picture carries no legend; the website's caption and lightbox
 * name each number, in the words of `SITE_CALLOUTS`.
 */
export function calloutScript(marks: Callout[]): string {
  return `(() => {
    document.activeElement?.blur?.()
    document.querySelectorAll(".site-callout").forEach((n) => n.remove())
    const marks = ${JSON.stringify(marks)}
    const accent = "#ff8a5c"
    const font = "-apple-system, BlinkMacSystemFont, 'PingFang TC', 'Helvetica Neue', sans-serif"
    const NS = "http://www.w3.org/2000/svg"
    const svg = document.createElementNS(NS, "svg"); svg.setAttribute("class", "site-callout")
    svg.setAttribute("width", innerWidth); svg.setAttribute("height", innerHeight)
    Object.assign(svg.style, { position: "fixed", inset: "0", zIndex: 2147483645, pointerEvents: "none" })
    const mask = document.createElementNS(NS, "mask"); mask.id = "site-callout-holes"
    const all = document.createElementNS(NS, "rect"); all.setAttribute("width", "100%"); all.setAttribute("height", "100%"); all.setAttribute("fill", "white"); mask.appendChild(all)
    svg.appendChild(mask)
    const shade = document.createElementNS(NS, "rect"); shade.setAttribute("width", "100%"); shade.setAttribute("height", "100%")
    shade.setAttribute("fill", "rgba(0,0,0,.38)"); shade.setAttribute("mask", "url(#site-callout-holes)"); svg.appendChild(shade)
    for (const m of marks) {
      const nodes = [...document.querySelectorAll(m.sel)].filter((n) => { const r = n.getBoundingClientRect(); return r.width && r.height })
      if (!nodes.length) throw new Error("nothing to mark at " + m.sel)
      const bs = nodes.map((n) => n.getBoundingClientRect()), pad = m.pad ?? 5
      const l = Math.max(4, Math.min(...bs.map((b) => b.left)) - pad), t = Math.max(4, Math.min(...bs.map((b) => b.top)) - pad)
      const r = Math.min(innerWidth - 4, Math.max(...bs.map((b) => b.right)) + pad), b = Math.min(innerHeight - 4, Math.max(...bs.map((b) => b.bottom)) + pad)
      const hole = document.createElementNS(NS, "rect")
      for (const [k, v] of Object.entries({ x: l, y: t, width: r - l, height: b - t, rx: 10, fill: "black" })) hole.setAttribute(k, v)
      mask.appendChild(hole)
      const box = document.createElementNS(NS, "rect")
      for (const [k, v] of Object.entries({ x: l + .75, y: t + .75, width: r - l - 1.5, height: b - t - 1.5, rx: 10, fill: "none", stroke: accent, "stroke-width": 1.5 })) box.setAttribute(k, v)
      svg.appendChild(box)
      const tag = document.createElement("span"); tag.className = "site-callout"; tag.textContent = String(m.n)
      Object.assign(tag.style, { position: "fixed", left: Math.max(2, l - 9) + "px", top: Math.max(2, t - 9) + "px", width: "18px", height: "18px",
        display: "grid", placeItems: "center", borderRadius: "50%", background: accent, color: "#1a0f0a", font: "700 11px/1 " + font,
        boxShadow: "0 0 0 2px rgba(10,10,12,.9)", zIndex: 2147483647, pointerEvents: "none" })
      document.body.appendChild(tag)
    }
    document.body.appendChild(svg)
    return true
  })()`
}

/** The callout words, shared by the pictures and the website's captions. */
export const SITE_CALLOUTS = {
  "attention-note": SITE_LANG === "en"
    ? ["The Agent's question", "Tap a reply to send it"]
    : ["Agent 問你的問題", "點一下就回覆"],
  "session-todo": SITE_LANG === "en"
    ? ["The next thing you queued for the Agent", "A screenshot that shows the problem"]
    : ["排給 Agent 的下一件事", "附上截圖說明問題"],
  "board-item": SITE_LANG === "en"
    ? ["How far the work has come", "The Session doing it, one tap away"]
    : ["這件工作走到哪一步", "負責的 Session，點一下就過去"],
} as const

// The website's feature pictures: two small stories, written separately in each
// language, and the callouts drawn over them. `site-shots.e2e.ts` serves the
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
 *
 * `squad` is the first case study's: a Session planning a refactor as an
 * architect, with three child Sessions in built-in roles under it, and the
 * code review coming back with a verdict.
 *
 * `trip` is the blog's story: a Session refactoring a trip planner's storage
 * gets a bug the person circled on their phone, a message from the Session
 * running the sync tests asking it to take a Board item, and asks the person
 * when to start.
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
    trip: {
      project: "Trip planner",
      refactor: "Storage refactor",
      sync: "Sync tests",
      calendar: "Calendar import",
      asked: "Move the trip data onto the new storage layer, one module at a time.",
      answered: "Three of the seven modules are on the new storage layer, and the tests pass after each move.",
      todo: "On my phone “Book hotel” isn't indented under “Kyoto trip”. Fix it after the refactor.",
      screenshot: "kyoto-list.png",
      picture: { title: "Trips", rows: [["Kyoto trip", 0], ["Book flights", 1], ["Book hotel", 0], ["Rail pass", 1]], circled: 2 },
      message: "While running the sync tests I found that the phone's direct connection drops when it switches networks and never comes back. I opened a Board item for it, “Reconnect directly after a network switch”. Could you take it? I asked first, and the owner agreed it should be yours.",
      considered: "Sync tests wants me to take the direct-connection item. One question before I touch it.",
      question: "Take the direct-connection item from Sync tests and start it now?",
      options: [
        ["Take it now", "Pause the refactor after the module in progress"],
        ["After the current work", "Finish the storage refactor first, then start it"],
        ["Not by me", "Leave it on the Board for another Session"],
      ],
    },
    squad: {
      project: "Budget app",
      coordinator: "Split the sync engine",
      children: {
        backend: "Extract conflict handling",
        checker: "Prove sync is unchanged",
        review: "Review the split",
      },
      asked: "The sync engine is one 3,000-line file. Split it into packages without changing what it does.",
      planned: "Three tasks are out: a backend engineer extracts conflict handling, a reality checker proves sync behaves exactly as before, and a code reviewer checks the split.",
      verdict: "The code review is back: proceed with findings. One finding: the new conflict package retries a rejected write with no limit, so a server that keeps refusing would keep the phone busy. I'll cap the retries before merging.",
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
    trip: {
      project: "旅遊規劃",
      refactor: "儲存層重構",
      sync: "同步測試",
      calendar: "行事曆匯入",
      asked: "把行程資料一個模組一個模組搬到新的儲存層。",
      answered: "七個模組已經搬好三個，每搬完一個，測試都還是全過。",
      todo: "手機上「訂飯店」沒有縮排在「京都之旅」底下，重構做完順手修掉。",
      screenshot: "京都行程.png",
      picture: { title: "我的行程", rows: [["京都之旅", 0], ["訂機票", 1], ["訂飯店", 0], ["買 JR Pass", 1]], circled: 2 },
      message: "跑同步測試時發現：手機切換網路之後，直接連線會斷掉，而且不會自己接回來。我開了一張 Board 項目「切換網路後自動重新直連」，可以請你接手嗎？我先問過了，專案負責人也同意交給你。",
      considered: "同步測試請我接直接連線那張項目，動手之前先問一件事。",
      question: "要接下同步測試開的直接連線項目，而且現在就開始嗎？",
      options: [
        ["現在接手", "手上這個模組搬完就先暫停重構"],
        ["手上的做完再接", "儲存層重構完成後再開始"],
        ["不要給我", "留在 Board 上，交給其他 Session"],
      ],
    },
    squad: {
      project: "記帳 App",
      coordinator: "拆分同步引擎",
      children: {
        backend: "抽出衝突處理",
        checker: "證明同步行為沒變",
        review: "審查這次拆分",
      },
      asked: "同步引擎整個塞在一個三千行的檔案裡。拆成幾個套件，行為不能變。",
      planned: "派出三個任務：後端工程師抽出衝突處理，驗證員證明同步行為跟拆分前一模一樣，程式碼審查員檢查拆分結果。",
      verdict: "程式碼審查回來了，結論是「可以繼續，但有發現」。發現一項：新的衝突處理套件被伺服器拒絕寫入時會無限重試，伺服器一直拒絕的話，手機就會一直忙。合併前我先把重試次數設上限。",
    },
  },
} as const

export const STORY = STORIES[SITE_LANG]

export type Callout = { sel: string; n: number; label: string; pad?: number }

/**
 * A page expression that marks the real elements' boxes on the live page: a
 * 1px outline at 70% opacity with a small number at its corner, and a light shade over
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
      for (const [k, v] of Object.entries({ x: l + .5, y: t + .5, width: r - l - 1, height: b - t - 1, rx: 10, fill: "none", stroke: accent, "stroke-opacity": .7, "stroke-width": 1 })) box.setAttribute(k, v)
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
  "todo-screenshot": SITE_LANG === "en"
    ? ["The bug you report, as a to-do for the Session", "Your screenshot, with the problem circled"]
    : ["你回報的 bug，排成這個 Session 的待辦", "你的截圖，問題用紅筆圈起來"],
  "session-message": SITE_LANG === "en"
    ? ["The Session that sent it", "What it asks this Session to take on"]
    : ["發訊息的 Session", "它請這個 Session 接手的工作"],
  "ask-person": SITE_LANG === "en"
    ? ["The Agent asks before it takes the work", "Your choices, one tap each"]
    : ["Agent 接手之前先問你", "你的選項，點一下就回答"],
  "squad-list": SITE_LANG === "en"
    ? ["The coordinating Session, planning as an architect", "The child Sessions it started, each in its own role"]
    : ["負責協調的 Session，以架構師角色規劃", "它派出的子 Session，各有各的角色"],
  "review-notice": SITE_LANG === "en"
    ? ["The code review child has finished", "Its verdict: proceed with findings"]
    : ["負責審查的子 Session 做完了", "它的結論：可以繼續，但有發現"],
} as const

/**
 * A page expression that resolves to an invented phone screenshot, as a PNG
 * `File` named `name`: a list in some app where one child row sits flush with
 * its parent, circled by hand in red. The crop is square and tight so the
 * circle still reads in a to-do dialog's small thumbnail.
 */
export function inventedScreenshotScript(name: string, picture: { title: string; rows: readonly (readonly [string, number])[]; circled: number }): string {
  return `(async () => {
    const picture = ${JSON.stringify(picture)}
    const size = 600, canvas = document.createElement("canvas"); canvas.width = size; canvas.height = size
    const g = canvas.getContext("2d")
    const font = "-apple-system, BlinkMacSystemFont, 'PingFang TC', 'Helvetica Neue', sans-serif"
    g.fillStyle = "#f6f6f8"; g.fillRect(0, 0, size, size)
    g.fillStyle = "#16161a"; g.font = "700 46px " + font; g.fillText(picture.title, 36, 86)
    const top = 132, step = 104
    picture.rows.forEach(([label, depth], i) => {
      const y = top + i * step, x = 36 + depth * 54
      g.fillStyle = "#ffffff"; g.beginPath(); g.roundRect(x - 12, y, size - x - 24, 86, 16); g.fill()
      g.strokeStyle = depth ? "#8a8a96" : "#3b6cf6"; g.lineWidth = 4
      g.beginPath(); g.arc(x + 26, y + 43, 15, 0, Math.PI * 2); g.stroke()
      g.fillStyle = "#16161a"; g.font = (depth ? "400 " : "600 ") + "34px " + font; g.fillText(label, x + 60, y + 55)
    })
    // A hand-drawn loop: a wobbling ellipse that overshoots where it began.
    const cy = top + picture.circled * step + 43
    g.strokeStyle = "#e5262b"; g.lineWidth = 10; g.lineCap = "round"; g.lineJoin = "round"; g.beginPath()
    for (let i = 0; i <= 112; i++) {
      const a = -2.6 + (i / 100) * Math.PI * 2, wobble = 1 + 0.035 * Math.sin(i * 0.9) + 0.02 * Math.cos(i * 2.3)
      const px = 300 + Math.cos(a) * 286 * wobble, py = cy + Math.sin(a) * 74 * wobble + i * 0.12
      if (i) g.lineTo(px, py); else g.moveTo(px, py)
    }
    g.stroke()
    const blob = await new Promise((ok) => canvas.toBlob(ok, "image/png"))
    return new File([blob], ${JSON.stringify(name)}, { type: "image/png", lastModified: Date.now() })
  })()`
}

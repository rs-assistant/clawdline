// The website's feature screenshots are taken from these e2e fixtures, so they
// show the real console with anonymous sample data. `tools/site-shots.sh` sets
// SITE_SHOTS to an output directory and SITE_SHOTS_LANG to `en` or `zh-Hant`;
// without SITE_SHOTS the capture tests skip and the fixtures behave exactly as
// before. docs/site-shots.md says how the pictures are used.

export const SITE_SHOTS = process.env.SITE_SHOTS || ""
export const SITE_LANG = SITE_SHOTS && process.env.SITE_SHOTS_LANG === "en" ? "en" : "zh-Hant"
export const siteSuffix = SITE_LANG === "en" ? "en" : "zh-Hant"

// The fixtures' sample content is English. A Traditional Chinese picture with
// English sample content reads as a half-translated product, so the words the
// three scenes show are translated as they leave the stand-in daemon.
const ZH_SAMPLE: [string, string][] = ([
  ["The report is ready. Please review the proposed date and the linked evidence.", "報告已完成，請檢查建議日期與附上的證據。"],
  ["Only the person knows the preferred date.", "只有你知道適合的日期。"],
  ["See the Session before assigning work.", "指派工作前先查看 Session。"],
  ["The release is waiting for a date.", "發布正在等待日期。"],
  ["Wednesday works for me.", "星期三可以。"],
  ["Tuesday works for me.", "星期二可以。"],
  ["Choose an informed owner", "選擇了解任務的負責人"],
  ["Coordinate the live deployment", "協調正式部署"],
  ["Repair the previous release", "修復上一版發布"],
  ["Bravo is still working", "Bravo 仍在工作"],
  ["Review the release note", "檢查發布說明"],
  ["Choose a release day", "選擇發布日期"],
  ["Delivery Session", "交付 Session"],
  ["Manager Session", "管理 Session"],
  ["Choose a date.", "請選擇日期。"],
  ["\"Wednesday\"", "\"星期三\""],
  ["\"Tuesday\"", "\"星期二\""],
] as [string, string][]).sort((a, b) => b[0].length - a[0].length)

/** A fixture's JSON body, with its sample content in the picture's language. */
export function siteText(body: string): string {
  if (!SITE_SHOTS || SITE_LANG !== "zh-Hant") return body
  for (const [from, to] of ZH_SAMPLE) body = body.split(from).join(to)
  return body
}

export type Callout = { sel: string; n: number; label: string; pad?: number }

/**
 * A page expression that draws the callouts over the live page at the real
 * elements' boxes: the rest of the screen is dimmed, each box carries its
 * number, and a legend at the bottom names each number. The website's caption
 * repeats the legend word for word.
 */
export function calloutScript(marks: Callout[]): string {
  return `(() => {
    document.activeElement?.blur?.()
    document.querySelectorAll(".site-callout").forEach((n) => n.remove())
    const marks = ${JSON.stringify(marks)}
    const font = "-apple-system, BlinkMacSystemFont, 'PingFang TC', 'Helvetica Neue', sans-serif"
    const NS = "http://www.w3.org/2000/svg"
    const svg = document.createElementNS(NS, "svg"); svg.setAttribute("class", "site-callout")
    svg.setAttribute("width", innerWidth); svg.setAttribute("height", innerHeight)
    Object.assign(svg.style, { position: "fixed", inset: "0", zIndex: 2147483645, pointerEvents: "none" })
    const mask = document.createElementNS(NS, "mask"); mask.id = "site-callout-holes"
    const all = document.createElementNS(NS, "rect"); all.setAttribute("width", "100%"); all.setAttribute("height", "100%"); all.setAttribute("fill", "white"); mask.appendChild(all)
    svg.appendChild(mask)
    const legend = document.createElement("div"); legend.className = "site-callout"
    Object.assign(legend.style, { position: "fixed", left: "12px", right: "12px", bottom: "14px", zIndex: 2147483647, pointerEvents: "none",
      display: "grid", gap: "8px", padding: "12px 14px", borderRadius: "16px", background: "rgba(20,12,9,.94)", border: "1.5px solid #ff8a5c",
      boxShadow: "0 10px 30px rgba(0,0,0,.6)", font: "600 14.5px/1.35 " + font, color: "#f5ece6" })
    const badge = (n, size) => '<span style="display:inline-grid;place-items:center;flex:none;width:' + size + 'px;height:' + size + 'px;border-radius:50%;background:#ff8a5c;color:#1a0f0a;font:800 ' + Math.round(size * .55) + 'px/1 ' + font + '">' + n + '</span>'
    for (const m of marks) {
      const nodes = [...document.querySelectorAll(m.sel)].filter((n) => { const r = n.getBoundingClientRect(); return r.width && r.height })
      if (!nodes.length) throw new Error("nothing to mark at " + m.sel)
      const bs = nodes.map((n) => n.getBoundingClientRect()), pad = m.pad ?? 6
      const l = Math.max(6, Math.min(...bs.map((b) => b.left)) - pad), t = Math.max(6, Math.min(...bs.map((b) => b.top)) - pad)
      const r = Math.min(innerWidth - 6, Math.max(...bs.map((b) => b.right)) + pad), b = Math.min(innerHeight - 6, Math.max(...bs.map((b) => b.bottom)) + pad)
      const hole = document.createElementNS(NS, "rect")
      for (const [k, v] of Object.entries({ x: l, y: t, width: r - l, height: b - t, rx: 12, fill: "black" })) hole.setAttribute(k, v)
      mask.appendChild(hole)
      const box = document.createElement("div"); box.className = "site-callout"
      Object.assign(box.style, { position: "fixed", left: l + "px", top: t + "px", width: (r - l) + "px", height: (b - t) + "px",
        border: "2.5px solid #ff8a5c", borderRadius: "12px", boxShadow: "0 0 18px rgba(255,138,92,.45)",
        zIndex: 2147483646, pointerEvents: "none", boxSizing: "border-box" })
      const tag = document.createElement("div"); tag.className = "site-callout"; tag.innerHTML = badge(m.n, 30)
      Object.assign(tag.style, { position: "fixed", left: Math.min(innerWidth - 34, r - 17) + "px", top: Math.max(2, t - 15) + "px",
        zIndex: 2147483647, pointerEvents: "none", borderRadius: "50%", boxShadow: "0 0 0 3px #111, 0 4px 12px rgba(0,0,0,.5)" })
      const row = document.createElement("div"); row.style.cssText = "display:flex;align-items:center;gap:10px"
      row.innerHTML = badge(m.n, 24); row.appendChild(document.createTextNode(m.label)); legend.appendChild(row)
      document.body.appendChild(box); document.body.appendChild(tag)
    }
    const shade = document.createElementNS(NS, "rect"); shade.setAttribute("width", "100%"); shade.setAttribute("height", "100%")
    shade.setAttribute("fill", "rgba(0,0,0,.5)"); shade.setAttribute("mask", "url(#site-callout-holes)"); svg.appendChild(shade)
    document.body.appendChild(svg); document.body.appendChild(legend)
    return true
  })()`
}

/** The callout words, shared by the pictures and the website's captions. */
export const SITE_CALLOUTS = {
  "attention-note": SITE_LANG === "en"
    ? ["What the Agent is asking you", "Tap a suggested reply to send it"]
    : ["Agent 在問你的問題", "點一下就送出建議回覆"],
  "session-todo": SITE_LANG === "en"
    ? ["A to-do for the Agent, with its status", "The picture you attached"]
    : ["留給 Agent 的待辦與狀態", "附上的參考圖"],
  "board-item": SITE_LANG === "en"
    ? ["Review before work starts?", "Choose the Session that does it"]
    : ["開工前要不要獨立審查", "交給哪個 Session 做"],
} as const

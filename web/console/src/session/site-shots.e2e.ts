// The website's three feature pictures, photographed from the built console
// against a stand-in daemon that serves one small story (`site-shots.ts`).
// `tools/site-shots.sh` builds the console and runs this once per language;
// without SITE_SHOTS every test skips. docs/site-shots.md is the whole of it.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { SITE_CALLOUTS, SITE_LANG, SITE_SHOTS, STORY, calloutScript, siteSuffix, type Callout } from "./site-shots.ts"

const dist = resolve(dirname(fileURLToPath(import.meta.url)), "../../dist")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const now = Math.floor(Date.now() / 1000)
const PROJECT_PATH = "~/code/shop"

const CHECKOUT = "site-checkout"
const SIGN_IN = "site-sign-in"
const SEARCH = "site-search"
const conversation = (id: string) => "conversation-" + id

// The Session asking the question is waiting on the person; the picture of the
// queued to-do shows it working again after the answer.
let checkoutWaiting = true

function session(id: string, label: string, minutesAgo: number, extra: Record<string, unknown> = {}) {
  return {
    id, label, backend: "tmux", state: "idle", work_state: "ready", evidence: "process",
    isClaude: true, assistant: "claude", sessionId: conversation(id), cwd: PROJECT_PATH,
    activity: { known: true, at: now - minutesAgo * 60 },
    closeability: { activity_generation: 1, attestation_id: null, mover: null, obligation_generation: 1, observed_at: 1,
      provenance: [], reasons: [], session_generation: 1, source: "broker", state: "safe", version: "site" },
    ...extra,
  }
}

const snapshot = () => ({
  at: Date.now(),
  scan: { complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1, provenance: "fixture" },
  sessions: [
    session(CHECKOUT, STORY.checkout, 1, checkoutWaiting
      ? { attention_count: 1, state: "waiting", work_state: "waiting_you" }
      : { state: "working", work_state: "working", line: "2m" }),
    session(SIGN_IN, STORY.signIn, 6, { assistant: "codex", isClaude: false, state: "working", work_state: "working", line: "6m" }),
    session(SEARCH, STORY.search, 40),
  ],
})

const project = { id: "site-shop", label: STORY.project, path: PROJECT_PATH, icon: null, available: true }

function item(id: string, kind: string, [title, description]: readonly [string, string], phase: string, owner: string | null, minutesAgo: number) {
  return {
    id, project, kind, title, description, phase, condition: null, area: phase === "created" ? "unassigned" : phase,
    deployment_policy: "agent_decides", review_required: false, owner_session: owner ? conversation(owner) : null,
    created_at: now - 86400, updated_at: now - minutesAgo * 60, phase_entered_at: now - minutesAgo * 60,
    closed_at: null, cycle: 1, version: 1,
  }
}

const items = () => [
  item("site-checkout-item", "feature", STORY.items.checkout, "implementing", CHECKOUT, 25),
  item("site-sign-in-item", "feature", STORY.items.signIn, "verifying", SIGN_IN, 12),
  item("site-search-item", "feature", STORY.items.search, "created", null, 90),
  item("site-reviews-item", "feature", STORY.items.reviews, "created", null, 300),
]

const note = () => ({
  id: "10000000-0000-4000-8000-000000000101", kind: "answer",
  source_conversation: conversation(CHECKOUT), source_label: STORY.checkout,
  target_conversation: conversation(CHECKOUT), target_session: CHECKOUT,
  ...STORY.note, options: [...STORY.note.options], document_url: "", detail: "",
  created_at: now - 60, read_at: null, resolved_at: null, version: 1,
})

const todo = () => ({
  id: "site-todo", text: STORY.todo, created_at: now - 120, sent_at: now - 110, read_at: now - 100, completed_at: null, version: 3,
  images: [{ id: "site-todo-image", title: STORY.screenshot, media_type: "image/png", byte_count: 68, width: 1, height: 1,
    position: 0, created_by: "site", created_at: now - 120 }],
})

const transcript = (id: string) => id === CHECKOUT ? [
  { role: "user", text: STORY.asked, at: now - 1800, kind: "message" },
  { role: "assistant", text: STORY.answered, at: now - 90, kind: "message" },
] : []

const TYPES: Record<string, string> = { ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".json": "application/json", ".png": "image/png", ".webp": "image/webp", ".ico": "image/x-icon" }
function json(res: ServerResponse, status: number, value: unknown) { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(value)) }
function page(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"; words.dir = "ltr"
  return html.replace("<!-- clawdline:strings -->", "<script>localStorage.setItem('ui_language'," + JSON.stringify(SITE_LANG) + ");window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>")
    .replace("<!-- clawdline:cloud -->", "")
}

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/strings") {
      const file = join(dist, "catalogs", (url.searchParams.get("lang") || SITE_LANG) + ".json")
      return existsSync(file) ? json(res, 200, JSON.parse(readFileSync(file, "utf8"))) : json(res, 404, { error: "not_found" })
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/sessions") return json(res, 200, snapshot())
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      return // held open, as the daemon's stream is
    }
    if (path === "/v1/transcript") {
      const id = url.searchParams.get("session") ?? ""
      return json(res, 200, { entries: transcript(id), evidence: "process", id, signature: "site" })
    }
    const about = /^\/v1\/sessions\/([^/]+)\/(info|git)$/.exec(path)
    if (about) {
      const row = snapshot().sessions.find((s) => s.id === decodeURIComponent(about[1]))
      if (!row) return json(res, 404, { error: "not_found" })
      if (about[2] === "git") return json(res, 200, { git: { ahead: 0, behind: 0, branch: "checkout-redesign", clean: true, head: "0123456789abcdef", files: [] } })
      return json(res, 200, { info: {
        session: { id: row.id, title: row.label, assistant: row.assistant, namingAssistant: row.assistant, sessionId: row.sessionId, cwd: PROJECT_PATH, model: "claude-opus-5-5" },
        models: [{ id: "claude-opus-5-5", name: "Opus 5.5" }], context: { usedPercent: 18 }, deploy: [], links: [],
      } })
    }
    if (path === "/v1/places") return json(res, 200, { places: [{ id: project.id, label: project.label, path: PROJECT_PATH,
      icon: { accent: "#e07a5f", cells: [["#e07a5f", "#e07a5f"], ["#e07a5f", "#141416"]] } }], assistants: [{ id: "claude", label: "Claude" }] })
    if (path.startsWith("/v1/work/v2/human-interventions/")) {
      const mine = decodeURIComponent(path).includes(CHECKOUT) && checkoutWaiting
      return json(res, 200, { ok: true, rows: mine ? [note()] : [], pruned_resolved: 0 })
    }
    const todos = /^\/v1\/work\/v2\/session-todos\/(.+)$/.exec(path)
    if (todos && req.method === "GET") {
      const mine = decodeURIComponent(todos[1]).includes(CHECKOUT) && !checkoutWaiting
      return json(res, 200, { ok: true, assigned_items: [], recent_items: [], direct_todos: mine ? [todo()] : [],
        open_decisions: [], decisions_error: null, truncated: false })
    }
    if (path === "/v1/work/v2/items" && req.method === "GET") {
      const rows = items()
      const counts: Record<string, number> = {}
      for (const row of rows) counts[row.area] = (counts[row.area] ?? 0) + 1
      return json(res, 200, { ok: true, rows, counts, truncated: false })
    }
    if (path === "/v1/work/v2/proposals") return json(res, 200, { rows: [], truncated: false })
    if (path === "/v1/work/decisions") return json(res, 200, { counts: {}, rows: [], next_cursor: null })
    if (path === "/v1/orchestrator/tasks") return json(res, 200, { at: Date.now(), tasks: [] })
    if (path === "/v1/work/v2/images/site-todo-image") {
      res.writeHead(200, { "content-type": "image/png" })
      return res.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"))
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: "not_found", detail: path })
    if (path === "/") { res.writeHead(200, { "content-type": "text/html; charset=utf-8" }); return res.end(page()) }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) return json(res, 404, {})
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" }); res.end(readFileSync(file))
  })
}

class Browser {
  private seq = 0
  private pending = new Map<number, { ok: (v: any) => void; fail: (e: Error) => void }>()
  private loads: string[] = []
  private ws: WebSocket
  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(String(ev.data))
      if (msg.id === undefined) { if (msg.method === "Page.loadEventFired") this.loads.push(msg.sessionId); return }
      const p = this.pending.get(msg.id); this.pending.delete(msg.id)
      if (msg.error) p?.fail(new Error(msg.error.message)); else p?.ok(msg.result)
    })
  }
  static async connect(url: string): Promise<Browser> {
    const ws = new WebSocket(url)
    await new Promise<void>((ok, fail) => { ws.addEventListener("open", () => ok()); ws.addEventListener("error", () => fail(new Error("Chrome unreachable"))) })
    return new Browser(ws)
  }
  send(method: string, params: object = {}, sessionId?: string): Promise<any> {
    const id = ++this.seq; this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((ok, fail) => {
      const timer = setTimeout(() => fail(new Error(method + " timed out")), 15_000)
      this.pending.set(id, { ok: (v) => { clearTimeout(timer); ok(v) }, fail: (e) => { clearTimeout(timer); fail(e) } })
    })
  }
  async loaded(sessionId: string, before: number): Promise<void> {
    const deadline = Date.now() + 10_000
    while (!this.loads.slice(before).includes(sessionId)) { if (Date.now() > deadline) throw new Error("page did not load"); await new Promise((r) => setTimeout(r, 50)) }
  }
  get loadCount() { return this.loads.length }
  close() { this.ws.close() }
}

let server: Server, browserProcess: ChildProcess, browser: Browser, profile: string, origin: string
before(async () => {
  if (!SITE_SHOTS) return
  assert.ok(existsSync(join(dist, "index.html")), "build the console first")
  assert.ok(existsSync(chrome), "Chrome is required")
  server = daemon(); await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address(); origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-site-shots-"))
  browserProcess = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "about:blank"])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""; const timer = setTimeout(() => { browserProcess.kill(); fail(new Error("Chrome did not start: " + said)) }, 20_000)
    browserProcess.stderr?.on("data", (chunk) => { said += String(chunk); const match = /DevTools listening on (ws:\/\/\S+)/.exec(said); if (match) { clearTimeout(timer); ok(match[1]) } })
  })
  browser = await Browser.connect(endpoint)
})
after(async () => {
  browser?.close()
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await Promise.race([gone, new Promise((ok) => setTimeout(ok, 5000))])
  }
  server?.closeAllConnections(); await new Promise<void>((ok) => server ? server.close(() => ok()) : ok())
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

/** Opens the console at `address` on a phone, runs `arrange`, marks `marks` and saves `<scene>-<lang>.png`. */
async function shoot(scene: keyof typeof SITE_CALLOUTS, address: string, arrange: (run: (expression: string) => Promise<any>, until: (expression: string) => Promise<void>) => Promise<void>, marks: Omit<Callout, "n" | "label">[]) {
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
  const { sessionId: id } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
  const run = async (expression: string) => {
    const { result, exceptionDetails } = await browser.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true }, id)
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }
  const until = async (expression: string) => {
    const deadline = Date.now() + 8000
    while (!(await run(expression))) { if (Date.now() > deadline) assert.fail("the page never reached " + expression); await new Promise((r) => setTimeout(r, 50)) }
  }
  try {
    await browser.send("Page.enable", {}, id); await browser.send("Runtime.enable", {}, id)
    await browser.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 2, mobile: true }, id)
    await browser.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] }, id)
    const mark = browser.loadCount; await browser.send("Page.navigate", { url: origin + address }, id); await browser.loaded(id, mark)
    await arrange(run, until)
    // Folds open with an animation; a picture taken during one clips the row.
    await new Promise((r) => setTimeout(r, 900))
    assert.equal(await run(`document.documentElement.lang.startsWith(${JSON.stringify(SITE_LANG === "en" ? "en" : "zh")})`), true, "the console is in the picture's language")
    const words = SITE_CALLOUTS[scene]
    await run(calloutScript(marks.map((m, index) => ({ ...m, n: index + 1, label: words[index] }))))
    await new Promise((r) => setTimeout(r, 200))
    const { data } = await browser.send("Page.captureScreenshot", { format: "png" }, id)
    writeFileSync(join(SITE_SHOTS, `${scene}-${siteSuffix}.png`), Buffer.from(data, "base64"))
  } finally { await browser.send("Target.closeTarget", { targetId }) }
}

test("site shot: the Agent asks, and a tap answers", { skip: !SITE_SHOTS }, async () => {
  checkoutWaiting = true
  await shoot("attention-note", "/#session=" + CHECKOUT, async (run, until) => {
    await until(`!!document.querySelector(".human-interventions-head")`)
    await run(`document.querySelector(".human-interventions-head").click()`)
    await until(`!!document.querySelector(".human-intervention-option")`)
  }, [
    { sel: ".human-intervention-card h3, .human-intervention-card > p:not(.human-intervention-source)" },
    { sel: ".human-intervention-option" },
  ])
})

test("site shot: the next request, queued with a screenshot", { skip: !SITE_SHOTS }, async () => {
  checkoutWaiting = false
  try {
    await shoot("session-todo", "/#session=" + CHECKOUT, async (run, until) => {
      await until(`(() => {
        const fold = document.querySelector('.session-todos')
        if (fold && !fold.open) fold.querySelector('summary')?.click()
        return !!document.querySelector('.session-todo-image-link')?.getAttribute('href')?.startsWith('blob:')
      })()`)
    }, [
      { sel: ".session-direct-todo", pad: 2 },
      { sel: ".session-todo-image-link", pad: 3 },
    ])
  } finally { checkoutWaiting = true }
})

test("site shot: a Board item shows its stage and its Session", { skip: !SITE_SHOTS }, async () => {
  await shoot("board-item", "/#page=work", async (run, until) => {
    await until(`!!document.querySelector('#work [data-work-id="site-checkout-item"] .work-card-summary')`)
    await run(`document.querySelector('#work [data-work-id="site-checkout-item"] .work-card-summary').click()`)
    await until(`!!document.querySelector('.work-item-detail-modal .work-session-link')`)
  }, [
    { sel: ".work-item-detail-modal .work-milestones" },
    { sel: ".work-item-detail-modal .work-session-link", pad: 4 },
  ])
})

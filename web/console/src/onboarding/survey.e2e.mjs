// Run through tools/heavy.sh with PLAYWRIGHT_MODULE pointing to an installed Playwright module.
// The real form/coordinator runs in Vite; a disposable HTTP server supplies controlled receipts.
import { test } from "node:test"
import assert from "node:assert/strict"
import { createServer } from "node:http"
import { spawn } from "node:child_process"
import { existsSync, writeFileSync, unlinkSync, mkdirSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { resolve } from "node:path"
import { once } from "node:events"

const root = fileURLToPath(new URL("../../../../", import.meta.url))
const fixture = resolve(root, "web/console/survey-fixture.html")
const entry = resolve(root, "web/console/survey-fixture.tsx")
const empty = () => ({ version: 1, revision: 0, status: "new", answers: null })

test("bilingual desktop/mobile survey, settings edits, local consent and lost-receipt retry", async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  let cloud = empty(), local = empty(), loseReceipt = false, failStats = false
  const requests = [], submittedKeys = new Set()
  const api = createServer(async (req, res) => {
    const origin = req.headers.origin || "*"
    res.setHeader("access-control-allow-origin", origin)
    res.setHeader("access-control-allow-credentials", "true")
    res.setHeader("access-control-allow-methods", "GET,POST,OPTIONS")
    res.setHeader("access-control-allow-headers", "content-type,idempotency-key")
    if (req.method === "OPTIONS") { res.writeHead(204); res.end(); return }
    requests.push({ method: req.method, path: req.url, key: req.headers["idempotency-key"] })
    if (req.url.startsWith("/su/v1/onboarding/statistics")) {
      res.setHeader("content-type", "application/json")
      if (failStats) { res.writeHead(503); res.end(JSON.stringify({error:{code:"survey_statistics_unavailable"}})); return }
      res.end(JSON.stringify({schema:"clawdline.onboarding.statistics.v1",generated_at:new Date().toISOString(),shown:2,submitted:1,skipped:1,paired:1,
        reasons:[{key:"phone",count:1}],tools:[{key:"claude",count:1},{key:"codex",count:1}],sources:[{key:"github",count:1}],
        reason_tools:[{reason:"phone",tool:"codex",count:1}],source_reasons:[{source:"github",reason:"phone",count:1}]})); return
    }
    if (req.method === "GET") { res.setHeader("content-type", "application/json"); res.end(JSON.stringify(cloud)); return }
    let raw = ""; for await (const chunk of req) raw += chunk
    const body = JSON.parse(raw)
    const isLocal = req.url.endsWith("/local")
    let state = isLocal ? local : cloud
    if (body.action === "submit") {
      if (isLocal) { assert.equal(body.consent, true); assert.ok(body.respondent); assert.equal(req.headers.cookie, undefined) }
      if (!submittedKeys.has(req.headers["idempotency-key"])) {
        state = { version: 1, revision: state.revision + 1, status: "submitted", answers: body.answers }
        submittedKeys.add(req.headers["idempotency-key"])
      }
    } else if (state.status !== "submitted") state = { ...state, revision: state.revision + 1, status: body.action === "skip" ? "skipped" : "shown" }
    if (isLocal) local = state; else cloud = state
    if (loseReceipt && isLocal) { loseReceipt = false; res.setHeader("content-type", "application/json"); res.end('{"incomplete":'); return }
    res.setHeader("content-type", "application/json"); res.end(JSON.stringify(state))
  })
  api.listen(0, "127.0.0.1"); await once(api, "listening")
  const apiOrigin = `http://127.0.0.1:${api.address().port}`
  let vite, browser
  assert.ok(!existsSync(fixture) && !existsSync(entry), "fixture paths must be task-owned")
  try {
    writeFileSync(fixture, '<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#171614;color:#f2efea;font-family:system-ui;padding:24px;box-sizing:border-box}*{box-sizing:border-box}</style></head><body><div id="root"></div><script type="module" src="/survey-fixture.tsx"></script></body></html>')
    writeFileSync(entry, `import React,{useEffect} from 'react';import {createRoot} from 'react-dom/client';import {bootCatalog} from './src/catalog';import {SurveyStatistics} from './src/onboarding/Statistics';import {SurveyCoordinator,SurveySettingsLink,SESSION_READ} from './src/onboarding/Survey';import './src/legacy/tokens.css';
const p=new URLSearchParams(location.search);localStorage.setItem('ui_language',p.get('lang')||'en');
await bootCatalog(tag=>'/catalogs/'+tag+'.json');
function Fixture(){useEffect(()=>{dispatchEvent(new Event(SESSION_READ))},[]);return p.has('stats')?<SurveyStatistics apiOrigin="${apiOrigin}"/>:<><SurveySettingsLink/><SurveyCoordinator productVersion="fixture-v1" localApiOrigin="${apiOrigin}" account={p.has('cloud')?{id:'fixture-account',apiOrigin:'${apiOrigin}'}:undefined} eligible={p.has('cloud')}/></>};createRoot(document.getElementById('root')!).render(<Fixture/>);`)
    vite = spawn(process.execPath, [resolve(root, "web/node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "0"], { cwd: resolve(root, "web/console"), stdio: ["ignore", "pipe", "pipe"] })
    const origin = await new Promise((yes, no) => {
      const listen = chunk => { const match = String(chunk).match(/http:\/\/127\.0\.0\.1:\d+/); if (match) yes(match[0]) }
      vite.stdout.on("data", listen); vite.stderr.on("data", chunk => process.stderr.write(chunk)); vite.once("exit", code => no(new Error("Vite exited: " + code)))
    })
    browser = await chromium.launch({ executablePath: process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", headless: true })
    const page = await browser.newPage({ viewport: { width: 1440, height: 1080 } })
    const errors = []; page.on("pageerror", e => errors.push(String(e)))
    const fill = async (page, reason = "phone") => {
      await page.locator("#survey-reason").selectOption(reason)
      await page.locator(".survey-tools summary").click()
      await page.getByLabel("Claude Code", { exact: true }).check()
      await page.getByLabel("Codex", { exact: true }).check()
      await page.locator(".survey-tools summary").click()
      await page.locator("#survey-source").selectOption("github")
    }
    await page.goto(origin + "/survey-fixture.html?cloud=1")
    await page.locator("dialog[open]").waitFor()
    await fill(page)
    if (process.env.CLAWDLINE_SHOTS) { mkdirSync(process.env.CLAWDLINE_SHOTS, { recursive: true }); await page.screenshot({ path: resolve(process.env.CLAWDLINE_SHOTS, "survey-en-desktop.png") }); await page.setViewportSize({width:390,height:844}); await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))); console.log("English phone layout", await page.locator("dialog").evaluate(e => ({dialogs:document.querySelectorAll("dialog").length,cards:document.querySelectorAll(".survey-card").length,rect:e.getBoundingClientRect().toJSON(),scroll:e.scrollHeight}))); await page.screenshot({path:resolve(process.env.CLAWDLINE_SHOTS,"survey-en-phone.png")}); await page.setViewportSize({width:1440,height:1080}) }
    await page.getByRole("button", { name: "Submit and continue", exact: true }).click()
    await page.getByRole("status").filter({ hasText: "Thank you" }).waitFor()
    assert.equal(cloud.answers.reason, "phone")
    assert.deepEqual(cloud.answers.tools, ["claude", "codex"])
    await page.reload(); await page.getByRole("button", { name: "Review or update your answers" }).click()
    await page.locator("dialog[open]").waitFor(); assert.equal(await page.locator("#survey-reason").inputValue(), "phone")
    await page.locator("#survey-reason").selectOption("agents")
    await page.getByRole("button", { name: "Submit and continue", exact: true }).click()
    await page.getByRole("status").filter({ hasText: "Thank you" }).waitFor(); assert.equal(cloud.answers.reason, "agents")
    const localPage = await browser.newPage()
    await localPage.goto(origin + "/survey-fixture.html")
    await localPage.getByRole("button", { name: "Take the survey" }).waitFor()
    const before = requests.length
    await localPage.getByRole("button", { name: "Not now" }).click()
    await localPage.getByRole("button", { name: "Review or update your answers" }).click()
    await fill(localPage)
    assert.equal(requests.length, before, "local display, dismissal and editing send no API requests")
    loseReceipt = true
    await localPage.getByRole("button", { name: "Submit and continue", exact: true }).click()
    await localPage.getByRole("alert").waitFor()
    await localPage.getByRole("button", { name: "Submit and continue", exact: true }).click()
    await localPage.getByRole("status").filter({ hasText: "Thank you" }).waitFor()
    const localCalls = requests.filter(r => r.path.endsWith("/local"))
    assert.equal(localCalls.length, 2); assert.equal(localCalls[0].key, localCalls[1].key); assert.equal(local.revision, 1)
    await localPage.reload(); await localPage.getByRole("button", { name: "Review or update your answers" }).click()
    await localPage.locator("dialog[open]").waitFor(); assert.equal(await localPage.locator("#survey-reason").inputValue(), "phone")
    cloud = empty()
    const phone = await browser.newPage({ viewport: { width: 390, height: 844 } })
    await phone.goto(origin + "/survey-fixture.html?cloud=1&lang=zh-Hant")
    await phone.locator("dialog[open]").waitFor()
    assert.match(await phone.locator("h1").innerText(), /讓 Clawdline/)
    await fill(phone)
    await phone.locator(".survey-tools summary").click()
    await phone.getByLabel("還沒開始使用 AI 開發工具", { exact: true }).check()
    assert.equal(await phone.getByLabel("Codex", { exact: true }).isChecked(), false)
    await phone.locator(".survey-tools summary").click()
    assert.ok(await phone.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "no phone horizontal overflow")
    if (process.env.CLAWDLINE_SHOTS) {
      await phone.screenshot({ path: resolve(process.env.CLAWDLINE_SHOTS, "survey-zh-phone.png") })
      await phone.setViewportSize({width:1440,height:1080}); await phone.screenshot({path:resolve(process.env.CLAWDLINE_SHOTS,"survey-zh-desktop.png")}); await phone.setViewportSize({width:390,height:844})
    }
    await phone.getByRole("button", { name: "送出並繼續", exact: true }).click()
    await phone.getByRole("status").filter({ hasText: "你的回答已儲存" }).waitFor()
    assert.deepEqual(cloud.answers.tools, ["not_started"])
    const statsPage = await browser.newPage({viewport:{width:1440,height:1080}})
    await statsPage.goto(origin + "/survey-fixture.html?stats=1")
    await statsPage.locator(".survey-metrics").waitFor()
    assert.match(await statsPage.locator(".survey-stats").innerText(), /Sample: 1 respondents/)
    assert.equal(await statsPage.locator(".survey-chart").count(), 5)
    failStats = true
    await statsPage.getByRole("button", {name:"Apply filters"}).click()
    await statsPage.getByRole("alert").waitFor()
    assert.equal(await statsPage.locator(".survey-metrics").count(), 0, "failed refresh never fabricates zeros or presents stale counts as fresh")
    assert.deepEqual(errors, [])
    // Exercise native dialog and checkbox interaction in WebKit as well.
    if (process.env.CLAWDLINE_WEBKIT === "1") {
      const safari = await webkit.launch()
      try {
        const tab = await safari.newPage({ viewport: { width: 390, height: 844 } })
        await tab.goto(origin + "/survey-fixture.html?lang=zh-Hant")
        await tab.getByRole("button", { name: "填寫問卷", exact: true }).click()
        await tab.locator("dialog[open]").waitFor(); await fill(tab)
        await tab.getByRole("button", { name: "送出並繼續", exact: true }).click()
        await tab.getByRole("status").filter({ hasText: "你的回答已儲存" }).waitFor()
      } finally { await safari.close() }
    }
  } finally {
    await browser?.close()
    if (vite && vite.exitCode === null) { vite.kill("SIGTERM"); await once(vite, "exit") }
    await new Promise(done => api.close(done))
    if (existsSync(fixture)) unlinkSync(fixture)
    if (existsSync(entry)) unlinkSync(entry)
  }
})

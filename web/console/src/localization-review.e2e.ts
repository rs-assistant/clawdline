// Run explicitly; this review matrix is not part of the unit suite.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { resolve, join } from 'node:path'
import { parseArgs } from 'node:util'
import { randomUUID } from 'node:crypto'

type ReviewState = 'ready' | 'empty' | 'failed' | 'offline' | 'signed-out' | 'forbidden' | 'pairing' | 'stale'
type Surface = { id: string; origin: 'app' | 'cloud'; path: string; state: ReviewState; ready: string; action?: string }
type ReviewManifest = { repo: string; out: string; appOrigin: string; cloudOrigin: string; synthetic: boolean; unsupported: string[] }

export const REVIEW_SURFACES: Surface[] = [
  { id: 'sessions', origin: 'app', path: '/', state: 'ready', ready: '#rows > .row' },
  { id: 'navigation', origin: 'app', path: '/', state: 'ready', ready: '#brand', action: 'navigation' },
  { id: 'session-detail-composer', origin: 'app', path: '/#session=review-session', state: 'ready', ready: '#composer' },
  { id: 'session-plan-card', origin: 'app', path: '/#session=review-session', state: 'ready', ready: '.plan-card' },
  ...['settings', 'workflow', 'notifications', 'work', 'projects', 'plan', 'documents'].map(id => ({ id, origin: 'app' as const, path: '/#page=' + id, state: 'ready' as const, ready: `[data-page-view="${id}"]:not([hidden])` })),
  { id: 'machine-language-menu', origin: 'app', path: '/settings.html', state: 'ready', ready: '[role="tablist"]', action: 'machine-language-menu' },
  { id: 'machine-voice', origin: 'app', path: '/settings.html', state: 'ready', ready: '[role="tablist"]', action: 'machine-voice' },
  { id: 'cloud-pairing-open', origin: 'cloud', path: '/', state: 'pairing', ready: '#cloud-pair-start', action: 'cloud-pairing-open' },
  { id: 'send-unknown', origin: 'app', path: '/#session=review-session', state: 'ready', ready: '#composer', action: 'send-unknown' },
  { id: 'machine-settings', origin: 'app', path: '/settings.html', state: 'ready', ready: '[role="tablist"]' },
  { id: 'workflow-editor', origin: 'app', path: '/#page=workflow', state: 'ready', ready: '.workflow-primary', action: 'workflow-editor' },
  { id: 'board-modal', origin: 'app', path: '/#page=work', state: 'ready', ready: '#work .work-head-tools button', action: 'board-modal' },
  { id: 'project-files', origin: 'app', path: '/#page=projects', state: 'ready', ready: '.project-row-settings', action: 'project-files' },
  { id: 'document-detail', origin: 'app', path: '/#document=1&machine=this-mac&session=review-session&scope=project&path=README.md', state: 'ready', ready: '#document-body' },
  { id: 'role-picker', origin: 'app', path: '/', state: 'ready', ready: '#start-go', action: 'role-picker' },
  { id: 'squad', origin: 'app', path: '/#page=squad', state: 'ready', ready: '.squad-persona-card' },
  { id: 'squad-role-detail', origin: 'app', path: '/#page=squad', state: 'ready', ready: '.squad-persona-card', action: 'squad-role-detail' },
  { id: 'squad-skill-detail', origin: 'app', path: '/#page=squad', state: 'ready', ready: '.squad-persona-card', action: 'squad-skill-detail' },
  { id: 'sessions-empty', origin: 'app', path: '/', state: 'empty', ready: '#app' },
  { id: 'sessions-error', origin: 'app', path: '/', state: 'failed', ready: '#app' },
  { id: 'sessions-offline', origin: 'app', path: '/', state: 'offline', ready: '#app' },
  { id: 'cloud-sign-in', origin: 'cloud', path: '/', state: 'signed-out', ready: '#cloud-sign-in' },
  { id: 'cloud-pairing', origin: 'cloud', path: '/', state: 'pairing', ready: '[data-cloud-screen="pairing"]' },
  { id: 'cloud-error', origin: 'cloud', path: '/', state: 'failed', ready: '[data-cloud-screen="retrying"], [data-cloud-screen="failed"]' },
  ...['overview', 'accounts', 'promotions', 'usage'].map(id => ({ id: 'admin-' + id, origin: 'cloud' as const, path: id === 'overview' ? '/admin' : '/admin/' + id, state: 'ready' as const, ready: '.adm-demo-control' })),
  { id: 'admin-signed-out', origin: 'cloud', path: '/admin', state: 'signed-out', ready: '.adm-notice a[href*="oauth"]' },
  { id: 'admin-forbidden', origin: 'cloud', path: '/admin', state: 'forbidden', ready: '.adm-notice h2' },
  { id: 'admin-failed', origin: 'cloud', path: '/admin', state: 'failed', ready: '.adm-notice h2' },
  { id: 'admin-demo', origin: 'cloud', path: '/admin', state: 'ready', ready: '.adm-demo-control', action: 'demo' },
  { id: 'admin-navigation', origin: 'cloud', path: '/admin', state: 'ready', ready: '.adm-demo-control', action: 'admin-navigation' },
  { id: 'admin-stale', origin: 'cloud', path: '/admin', state: 'stale', ready: '.adm-demo-control', action: 'stale' },
]

const { values } = parseArgs({ strict: true, options: Object.fromEntries(['repo', 'manifest', 'out', 'locales', 'widths', 'surfaces', 'playwright', 'browser', 'repeat', 'webcrypto', 'fault'].map(key => [key, { type: 'string' as const }])) })
const env = process.env
const manifestPath = values.manifest || env.REVIEW_MANIFEST
if (!manifestPath) throw new Error('--manifest or REVIEW_MANIFEST is required; start localization-review.mjs --serve first')
const manifest: ReviewManifest = JSON.parse(await readFile(resolve(manifestPath), 'utf8'))
assert.equal(manifest.synthetic, true, 'Refusing a non-fixture origin')
for (const origin of [manifest.appOrigin, manifest.cloudOrigin]) assert.equal(new URL(origin).hostname, '127.0.0.1', 'Review must stay on loopback')
const repo = resolve(values.repo || env.REVIEW_REPO || manifest.repo)
const output = resolve(values.out || env.REVIEW_OUTPUT || 'localization-review-output')
await mkdir(output, { recursive: true })
const locales = (values.locales || env.REVIEW_LOCALES || 'en,zh-Hant,ja,zh-Hans,ko,es,pt-BR,fr,de').split(',')
const widths = (values.widths || env.REVIEW_WIDTHS || '390,1280,320').split(',').map(Number)
assert.ok(widths.every(width => Number.isInteger(width) && width >= 240 && width <= 2560), 'Invalid viewport')
const selected = (values.surfaces || env.REVIEW_SURFACES || '').split(',').filter(Boolean)
const surfaces = selected.length ? selected.map(id => { const found = REVIEW_SURFACES.find(s => s.id === id); assert.ok(found, `Unknown surface ${id}`); return found }) : REVIEW_SURFACES
const require = createRequire(join(repo, 'web/package.json'))
// Optional module path is configuration only; no personal path is shipped.
const playwright = require(values.playwright || env.REVIEW_PLAYWRIGHT_MODULE || 'playwright')
const engine = values.browser || env.REVIEW_BROWSER || 'chromium'
assert.ok(['chromium', 'webkit', 'firefox'].includes(engine), 'Unknown browser engine')
const english = JSON.parse(await readFile(join(manifest.out, 'app/catalogs/en.json'), 'utf8'))
const fixtureData = JSON.parse(await readFile(join(manifest.out, 'fixture-data.json'), 'utf8'))
const names: Record<string, string> = { en: 'English', 'zh-Hant': '繁體中文', ja: '日本語', 'zh-Hans': '简体中文', ko: '한국어', es: 'Español', 'pt-BR': 'Português (Brasil)', fr: 'Français', de: 'Deutsch' }
const results: Record<string, unknown>[] = []
const repeat = Number(values.repeat || env.REVIEW_REPEAT || 1)
assert.ok(Number.isInteger(repeat) && repeat >= 1 && repeat <= 10, 'Repeat must be 1..10')
const fault = values.fault || ''
assert.ok(['', 'reject-catalog', 'key-id'].includes(fault), 'Unknown negative control')

/** Geometry candidates are evidence for review, not a semantic overlap oracle. */
export async function reviewDOM(page: any, keys: string[]) {
  return page.evaluate((catalogKeys: string[]) => {
    const visible = (element: Element) => { const r = element.getBoundingClientRect(); const s = getComputedStyle(element); return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none' && !element.closest('[hidden],[inert]') }
    const controls = [...document.querySelectorAll('button,input,textarea,select,a[href],[role="button"]')].filter(visible)
    const geometry = controls.map(element => {
      const rect = element.getBoundingClientRect()
      const x = rect.left + rect.width / 2, y = rect.top + rect.height / 2
      const hit = x >= 0 && y >= 0 && x < innerWidth && y < innerHeight ? document.elementFromPoint(x, y) : null
      return { tag: element.tagName, id: element.id, label: element.getAttribute('aria-label') || element.textContent?.trim() || '',
        rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
        horizontalEscape: rect.left < -1 || rect.right > innerWidth + 1,
        centerOccluded: !!hit && !element.contains(hit) && !hit.contains(element) }
    })
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
    const keySet = new Set(catalogKeys)
    const unexpectedKeys: string[] = []
    const textBoxes: { text: string; parent: Element; rect: DOMRect }[] = []
    let node: Node | null
    while ((node = walker.nextNode())) {
      if (!node.parentElement || !visible(node.parentElement) || node.parentElement.closest('script,style')) continue
      const text = node.textContent?.trim() || ''
      if (keySet.has(text) || /\b(?:literal|template)\.[a-f0-9]{12}\b/u.test(text)) unexpectedKeys.push(text)
      if (text) {
        const range = document.createRange()
        range.selectNodeContents(node)
        for (const rect of range.getClientRects()) if (rect.width && rect.height) textBoxes.push({ text, parent: node.parentElement, rect })
      }
    }
    for (const element of controls) for (const attribute of ['aria-label', 'title', 'placeholder']) {
      const text = element.getAttribute(attribute) || ''
      if (keySet.has(text)) unexpectedKeys.push(text)
    }
    const textOverlapCandidates: { a: string; b: string; width: number; height: number }[] = []
    for (let i = 0; i < textBoxes.length; i++) for (let j = i + 1; j < textBoxes.length; j++) {
      const a = textBoxes[i], b = textBoxes[j]
      if (a.parent.contains(b.parent) || b.parent.contains(a.parent)) continue
      const width = Math.min(a.rect.right, b.rect.right) - Math.max(a.rect.left, b.rect.left)
      const height = Math.min(a.rect.bottom, b.rect.bottom) - Math.max(a.rect.top, b.rect.top)
      if (width > 1 && height > 1) textOverlapCandidates.push({ a: a.text, b: b.text, width, height })
    }
    return { lang: document.documentElement.lang, missing: document.documentElement.getAttribute('data-i18n-missing'),
      viewport: innerWidth, documentWidth: document.documentElement.scrollWidth, text: document.body.innerText,
      unexpectedKeys, geometry, textOverlapCandidates, headings: [...document.querySelectorAll('h1,h2,h3')].filter(visible).map(x => x.textContent) }
  }, keys)
}

test('candidate localization browser review', { timeout: 3_000_000 }, async () => {
  const experimentalWebCrypto = (values.webcrypto || env.REVIEW_WEBCRYPTO) === 'experimental'
  const browser = await playwright[engine].launch({ headless: true,
    ...(engine === 'chromium' && experimentalWebCrypto ? { args: ['--enable-experimental-web-platform-features'] } : {}) })
  try {
    for (const locale of locales) {
      assert.ok(names[locale], `Unknown locale: ${locale}`)
      const catalog = JSON.parse(await readFile(join(manifest.out, 'app/catalogs', locale + '.json'), 'utf8'))
      const missing = Object.keys(english).filter(key => !(key in catalog))
      assert.deepEqual(missing, [], 'Full catalog required for this review; fallback is not acceptance')
      for (const width of widths) for (const surface of surfaces) for (let iteration = 1; iteration <= repeat; iteration++) {
        await (async () => {
          const context = await browser.newContext({ viewport: { width, height: 900 }, locale, ignoreHTTPSErrors: true })
          // No production API, analytics, model downloads or accidental outbound navigation.
          await context.route('**/*', (route: any) => {
            const url = new URL(route.request().url())
            return url.hostname === '127.0.0.1' || ['data:', 'blob:'].includes(url.protocol) ? route.continue() : route.abort('blockedbyclient')
          })
          if (fault) await context.route(/\/(?:v1\/strings\?|catalogs\/)/, async (route: any) => {
            if (new URL(route.request().url()).hostname !== '127.0.0.1') return route.abort('blockedbyclient')
            const response = await route.fetch()
            const catalog = await response.json()
            if (fault === 'reject-catalog') delete catalog['ui.language']
            else catalog['legacy.webSettings'] = 'legacy.webSettings'
            await route.fulfill({ response, json: catalog })
          })
          const page = await context.newPage()
          page.setDefaultTimeout(10_000)
          const actor = randomUUID()
          await context.addCookies([{ name: 'review_actor', value: actor, url: manifest.appOrigin }])
          const errors: string[] = []
          page.on('pageerror', (error: Error) => errors.push(error.stack || String(error)))
          const stem = `${locale}-${width}-${surface.id}-${iteration}`
          const result: Record<string, unknown> = { locale, width, surface: surface.id, iteration, fault, url: '', errors, status: 'failed' }
          try {
            // Select through the actual control and prove the real reload persists it.
            const origin = surface.origin === 'app' ? manifest.appOrigin : manifest.cloudOrigin
            await page.goto(origin + '/settings.html')
            await page.locator('#settings-window-ui-language').waitFor({ state: 'visible' })
            await page.locator('#settings-window-ui-language').click()
            await Promise.all([page.waitForEvent('load'), page.getByRole('option', { name: names[locale] }).click()])
            await page.waitForFunction(() => !document.documentElement.classList.contains('booting'))
            assert.equal(await page.locator('html').getAttribute('lang'), locale, 'Selected catalog activation fell back after reload')
            assert.equal(await page.evaluate(() => localStorage.getItem('ui_language')), locale)
            assert.equal(await page.locator('#settings-window-ui-language').getAttribute('aria-label'), catalog['ui.language'], 'Language control copy did not update after reload')
            await page.goto(origin + '/__review/state?state=' + surface.state)
            result.url = origin + surface.path
            await page.goto(result.url as string)
            await page.locator(surface.ready).first().waitFor({ state: 'visible' })
            await page.waitForFunction(() => !document.documentElement.classList.contains('booting'))
            if (surface.id === 'workflow') await page.locator('.workflow-list-card,.workflow-empty').first().waitFor({ state: 'visible' })
            if (surface.id === 'work') await page.locator('#work .work-empty,#work .work-card').first().waitFor({ state: 'visible' })
            if (surface.action === 'machine-language-menu') {
              await page.locator('#settings-window-ui-language').click()
              const menu = page.getByRole('listbox')
              await menu.waitFor({ state: 'visible' })
              const box = await menu.boundingBox()
              assert.ok(box && box.x >= 0 && box.x + box.width <= width, 'Expanded language menu stays inside the viewport')
            }
            if (surface.action === 'machine-voice') {
              await page.getByRole('tab').nth(1).click()
              await page.locator('.sw-voice-paths').first().waitFor({ state: 'visible' })
            }
            if (surface.action === 'cloud-pairing-open') {
              await page.locator('#cloud-pair-start').click()
              await page.locator('#cloud-pair-title').waitFor({ state: 'visible' })
            }
            if (surface.action === 'send-unknown') {
              await page.locator('#msg').fill('Synthetic review input')
              await page.locator('#send').click()
              const action = page.locator('.entry.pending[data-send="unknown"] [data-pending-look]')
              await action.waitFor({ state: 'visible' })
              const lines = await action.evaluate((node: Element) => {
                const range = document.createRange(); range.selectNodeContents(node)
                return range.getClientRects().length
              })
              assert.equal(lines, 1, 'Unknown-delivery action remains on one readable line')
            }
            if (surface.action === 'demo') {
              await page.locator('.adm-demo-control button').click()
              await page.locator('.adm-demo-active').waitFor({ state: 'visible' })
            }
            if (surface.action === 'navigation') {
              await page.locator('#brand').click()
              await page.locator('#sidebar [data-page-to="settings"]').click()
              await page.locator('#settings-ui-language-select').waitFor({ state: 'visible' })
              await page.locator('#brand').click()
              await page.locator('#sidebar').waitFor({ state: 'visible' })
            }
            if (surface.action === 'admin-navigation') {
              if (await page.locator('.adm-menu').isVisible()) await page.locator('.adm-menu').click()
              await page.locator('.adm-nav').waitFor({ state: 'visible' })
            }
            if (surface.action === 'stale') {
              await page.locator('.adm-shell button[aria-busy]:visible').click()
              await page.locator('.adm-stale').waitFor({ state: 'visible' })
            }
            if (surface.action === 'workflow-editor') {
              await page.locator('.workflow-primary').first().click()
              await page.locator('.workflow-editor[role="dialog"]').waitFor({ state: 'visible' })
            }
            if (surface.action === 'board-modal') {
              await page.locator('#work .work-head-tools button').first().click()
              await page.locator('.work-new-modal[role="dialog"]').waitFor({ state: 'visible' })
            }
            if (surface.action === 'project-files') {
              await page.locator('.project-row-settings').first().click()
              await page.locator('.project-setup-dialog[open] .project-setup-views button').last().click()
              await page.getByRole('button', { name: 'README.md', exact: true }).click()
              await page.locator('.project-explorer-text').waitFor({ state: 'visible' })
            }
            if (surface.action === 'role-picker') {
              await page.locator('#start-go').click()
              await page.locator('#start-persona .persona-chip').first().click()
              const chosen = page.locator('#start-persona .persona-chip[aria-pressed="true"]')
              await chosen.waitFor({ state: 'visible' })
              const id = await chosen.getAttribute('data-persona-id')
              const key = `builtinPersona.${id}.name`
              if (Object.hasOwn(catalog, key)) assert.ok((await chosen.innerText()).includes(catalog[key]), 'Built-in role picker kept another language')
            }
            if (surface.action === 'squad-role-detail' || surface.action === 'squad-skill-detail') {
              await page.locator('.squad-persona-card').first().click()
              await page.locator('#squad-detail-title').waitFor({ state: 'visible' })
              const definition = fixtureData.squadCatalog.definitions[0]
              const short = definition.short_id || definition.definition_id.replace(/^clawdline\.persona\./, '')
              if (Object.hasOwn(catalog, `builtinPersona.${short}.name`)) {
                assert.equal(await page.locator('#squad-detail-title').innerText(), catalog[`builtinPersona.${short}.name`], 'Built-in role detail kept another language')
              }
              if (surface.action === 'squad-skill-detail') {
                await page.locator('.squad-skill-card').first().click()
                await page.locator('#squad-skill-detail').waitFor({ state: 'visible' })
                const skillID = definition.skills[0].id.replace(/^clawdline\.skill\./, '')
                if (Object.hasOwn(catalog, `builtinSkill.${skillID}.name`)) {
                  assert.equal(await page.locator('#squad-skill-detail h4').innerText(), catalog[`builtinSkill.${skillID}.name`], 'Built-in Skill detail kept another language')
                  assert.equal(await page.locator('#squad-skill-detail dd').first().innerText(), catalog[`builtinSkill.${skillID}.purpose`], 'Built-in Skill purpose kept another language')
                }
                await page.locator('#squad-skill-detail').evaluate((element: HTMLElement) => element.scrollIntoView({ block: 'start' }))
              }
            }
            if (surface.id === 'machine-settings' && width <= 460) {
              const tabs = await page.locator('.sw-tab').evaluateAll((nodes: Element[]) => nodes.map(node => {
                const rect = node.getBoundingClientRect()
                return { width: rect.width, right: rect.right }
              }))
              assert.ok(tabs.length >= 5 && tabs.every((tab: { width: number; right: number }) => tab.width > 22 && tab.right <= width), 'All settings tabs need readable text columns')
            }
            await page.evaluate(() => document.fonts.ready)
            result.observedURL = page.url()
            const dom = await reviewDOM(page, Object.keys(english))
            result.dom = dom
            await writeFile(join(output, stem + '.html'), await page.content())
            assert.equal(dom.lang, locale, 'Actual catalog activation fell back')
            assert.equal(dom.missing, '0', 'Unexpected English fallback keys')
            assert.deepEqual(dom.unexpectedKeys, [], 'Catalog IDs escaped into the interface')
            assert.deepEqual(errors, [], 'Page runtime errors')
            assert.ok(dom.documentWidth <= width + 1, `Page overflow: ${dom.documentWidth}px in ${width}px viewport`)
            result.status = 'passed'
          } catch (error) { result.failure = String(error) }
          finally {
            await page.screenshot({ path: join(output, stem + '.png'), fullPage: true, animations: 'disabled' }).catch((error: Error) => { result.captureError = String(error) })
            if (!result.dom) result.dom = await reviewDOM(page, Object.keys(english)).catch(() => null)
            const origin = surface.origin === 'app' ? manifest.appOrigin : manifest.cloudOrigin
            const answer = await context.request.get(origin + '/__review/requests?actor=' + actor).then((res: any) => res.json()).catch(() => ({ requests: [] }))
            result.requests = answer.requests
            result.unsupportedRoutes = answer.requests.filter((request: any) => request.status === 404 || request.status === 405).map((request: any) => request.method + ' ' + request.path)
            await writeFile(join(output, stem + '.json'), JSON.stringify(result, null, 2) + '\n')
            results.push(result)
            console.log(`${result.status}: ${stem}${result.failure ? ' ' + result.failure : ''}`)
            await writeFile(join(output, 'report.json'), JSON.stringify({ candidate: manifest, engine, unsupported: manifest.unsupported, results }, null, 2) + '\n')
            await context.close()
          }
        })()
      }
    }
    assert.equal(results.filter(result => result.status !== 'passed').length, 0, 'Browser cells failed; see report.json')
  } finally { await browser.close() }
})

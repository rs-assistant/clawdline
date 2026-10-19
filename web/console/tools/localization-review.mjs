#!/usr/bin/env node
// Synthetic dependencies, actual candidate entry points and CSS. No live daemon.
import { createServer as httpServer } from 'node:http'
import { createServer as httpsServer } from 'node:https'
import { spawn, execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { readFile, writeFile, mkdir, stat, cp } from 'node:fs/promises'
import { resolve, join, extname, dirname, relative } from 'node:path'
import { pathToFileURL, fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'

export const REVIEW_LOCALES = ['en', 'zh-Hant', 'ja', 'zh-Hans', 'ko', 'es', 'pt-BR', 'fr', 'de']
export const REVIEW_STATES = ['ready', 'empty', 'failed', 'offline', 'signed-out', 'forbidden', 'pairing', 'stale']
export const REVIEW_SESSION = 'review-session'
const self = fileURLToPath(import.meta.url)
const mime = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2' }

export function reviewArgs(args) {
  const options = {}
  for (let i = 0; i < args.length; i++) {
    const key = args[i].replace(/^--/, '')
    if (['serve', 'build', 'help', 'builtins'].includes(key)) options[key] = true
    else if (args[i].startsWith('--') && args[i + 1] && !args[i + 1].startsWith('--')) options[key] = args[++i]
    else throw new Error(`Unknown or incomplete argument: ${args[i]}`)
  }
  return options
}

function run(command, args, options = {}) {
  return new Promise((yes, no) => {
    const child = spawn(command, args, { stdio: 'inherit', ...options })
    child.once('error', no)
    child.once('exit', (code, signal) => code === 0 ? yes() : no(new Error(`${command} exited ${code ?? signal}`)))
  })
}

export async function buildReview({ repo, out, cloudOrigin }) {
  assertOutsideCandidate(repo, out)
  const consoleRoot = join(repo, 'web/console')
  const require = createRequire(join(repo, 'web/package.json'))
  const { build } = await import(pathToFileURL(require.resolve('vite')).href)
  const { default: react } = await import(pathToFileURL(require.resolve('@vitejs/plugin-react')).href)
  // The three documented Vite documents. No source rewrite and no candidate writes.
  for (const hosted of [false, true]) {
    const declaration = JSON.stringify({ v: 1, app_origin: cloudOrigin, api_origin: cloudOrigin, relay_url: cloudOrigin.replace('https:', 'wss:') + '/fixture-relay' })
    await build({ configFile: false, root: consoleRoot, plugins: [react()], base: hosted ? '/' : './',
      define: { 'import.meta.env.VITE_HOSTED_CONSOLE': hosted ? JSON.stringify(declaration) : 'undefined' },
      build: { outDir: join(out, hosted ? 'cloud' : 'app'), emptyOutDir: true,
        rollupOptions: { input: Object.fromEntries(['index', 'bar', 'settings'].map(name => [name, join(consoleRoot, name + '.html')])) } },
    })
  }
}

function assertOutsideCandidate(repo, output) {
  const path = relative(resolve(repo), resolve(output))
  if (path === '' || (!path.startsWith('..') && !path.startsWith('/'))) throw new Error('Review output must be outside the read-only candidate')
}

const syntheticRow = () => ({ id: REVIEW_SESSION, label: 'Authored sample — release checklist', backend: 'owned', assistant: 'claude',
  state: 'idle', work_state: 'ready', evidence: 'process', isClaude: true,
  sessionId: '10000000-0000-4000-8000-000000000001', cwd: '/tmp/review-project' })
const snapshot = (state) => ({ at: Date.now(), scan: { complete: true, emptyAuthoritative: true, epoch: 1, generation: 1,
  completed: { complete: true, sequence: 1 }, provenance: 'synthetic-review' }, sessions: state === 'empty' ? [] : [syntheticRow()] })
const project = { id: 'review-project', label: 'Authored sample project', path: '/tmp/review-project', at: 1 }

export function sampleSquad() {
  const icon = { accent: '#D97757', cells: Array.from({ length: 7 }, () => Array.from({ length: 8 }, () => '#D97757')) }
  const names = text => ({ en: text, 'zh-Hant': text })
  const skill = { skill_id: 'review.skill.evidence', version: '1', source: 'synthetic-review', license: 'MIT', digest: 'synthetic',
    name: names('Authored sample evidence skill'), purpose: names('Authored sample: inspect the evidence before reporting.'),
    content: '# Authored sample skill\n\nRead the sample project. Report observed evidence.\n', icon, builtin: false }
  const definition = { definition_id: 'review.persona.reader', short_id: 'review-reader', version: '1', source: 'synthetic-review',
    license: 'MIT', digest: 'synthetic', name: names('Authored sample reader'), summary: names('Authored sample: review the interface and its evidence.'),
    body: '# Authored sample role\n\nRead the sample project and report observed behavior.\n', icon,
    teams: [{ id: 'review.team.quality', version: '1' }], skills: [{ id: skill.skill_id, version: skill.version, enabled: true }], builtin: false }
  return { catalog_version: 1, definitions: [definition], skills: [skill], teams: [{ team_id: 'review.team.quality', version: '1',
    name: names('Authored sample team'), personas: [{ id: definition.definition_id, version: definition.version }] }] }
}

export function reviewSquadSettings(catalog, scope = 'global') {
  const field = value => ({ value, source: 'default', present: false, version: 0 })
  return { scope_id: scope, scope_kind: scope === 'global' ? 'global' : 'repo', motion: field(false), motion_settings_version: 0,
    auto_dispatch: field(false), auto_dispatch_version: 0,
    personas: catalog.definitions.map(row => ({ definition_id: row.definition_id, settings_version: 0,
      handbook: field('Authored sample handbook. Preserve authored content.'), global_handbook: field('Authored sample handbook. Preserve authored content.'),
      auto_assign: field(true), skills: field(row.skills || []) })) }
}

/** Public JSON data, not parsed Go source; relationships remain explicit fixture data. */
export async function candidateBuiltinFixtures(repo) {
  const display = JSON.parse(await readFile(join(repo, 'web/console/src/builtin-display-source.json'), 'utf8'))
  const icon = { accent: '#D97757', cells: Array.from({ length: 7 }, () => Array.from({ length: 8 }, () => '#D97757')) }
  const ids = domain => [...new Set(Object.keys(display.en).filter(key => key.startsWith(domain + '.') && key.endsWith('.name')).map(key => key.slice(domain.length + 1, -5)))]
  const pair = key => ({ en: display.en[key], 'zh-Hant': display['zh-Hant'][key] })
  const valid = id => { if (!/^[a-z][a-z0-9-]*$/u.test(id)) throw new Error('Invalid public built-in display id'); return id }
  const skills = await Promise.all(ids('builtinSkill').map(async id => ({ skill_id: 'clawdline.skill.' + valid(id), version: 'review',
    name: pair(`builtinSkill.${id}.name`), purpose: pair(`builtinSkill.${id}.purpose`), icon, source: 'candidate-public-data', license: 'see authored source',
    content: await readFile(join(repo, 'internal/domain/squad/builtin_skills', id + '.md'), 'utf8'), builtin: true })))
  const definitions = await Promise.all(ids('builtinPersona').map(async id => ({ definition_id: 'clawdline.persona.' + valid(id), short_id: id, version: 'review',
    name: pair(`builtinPersona.${id}.name`), summary: pair(`builtinPersona.${id}.summary`), icon, source: 'candidate-public-data', license: 'see authored source',
    body: await readFile(join(repo, 'internal/domain/persona/catalog', id + '.md'), 'utf8'), builtin: true,
    teams: [{ id: 'clawdline.team.engineering', version: 'review' }], skills: skills.map(skill => ({ id: skill.skill_id, version: skill.version, enabled: true })) })))
  return { squadCatalog: { catalog_version: 1, definitions, skills, teams: [{ team_id: 'clawdline.team.engineering', version: 'review',
    name: { en: 'engineering', 'zh-Hant': 'engineering' }, personas: definitions.map(row => ({ id: row.definition_id, version: row.version })) }] } }
}

function json(response, status, value) {
  response.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' })
  response.end(JSON.stringify(value))
}

export async function serveReview(options) {
  const repo = resolve(options.repo)
  const out = resolve(options.out)
  const manifestFile = resolve(options.manifest)
  const servedOut = resolve(options.publish || out)
  const reused = options.reuse ? JSON.parse(await readFile(resolve(options.reuse), 'utf8')) : null
  if (reused && (!reused.synthetic || resolve(reused.out) !== servedOut)) throw new Error('--reuse requires a synthetic manifest and the same served output directory')
  assertOutsideCandidate(repo, out)
  assertOutsideCandidate(repo, manifestFile)
  assertOutsideCandidate(repo, servedOut)
  const candidateCommit = reused?.candidateCommit || execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
  const candidateStatus = reused ? '' : execFileSync('git', ['-C', repo, 'status', '--porcelain'], { encoding: 'utf8' })
  if (options['expect-commit'] && options['expect-commit'] !== candidateCommit) throw new Error('Candidate HEAD does not match --expect-commit')
  if (candidateStatus.trim()) throw new Error('Candidate must be clean so the rendered tree has a reviewable identity')
  await mkdir(out, { recursive: true })
  await mkdir(dirname(manifestFile), { recursive: true })
  const keyFile = join(out, 'loopback-key.pem'), certFile = join(out, 'loopback-cert.pem')
  try { await stat(certFile) } catch {
    await run('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', keyFile, '-out', certFile,
      '-days', '2', '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1,DNS:localhost'])
  }
  const { demoOverview } = await import(pathToFileURL(join(repo, 'web/console/src/cloud/admin/demo.ts')).href)
  if (options.fixtures && options.builtins) throw new Error('Choose --fixtures or --builtins')
  const providedFixtures = options.fixtures ? JSON.parse(await readFile(resolve(options.fixtures), 'utf8')) : options.builtins ? await candidateBuiltinFixtures(repo) : null
  const squadCatalog = providedFixtures?.squadCatalog || sampleSquad()
  if (!Array.isArray(squadCatalog.definitions) || !Array.isArray(squadCatalog.skills) || !Array.isArray(squadCatalog.teams)) throw new Error('--fixtures must contain a squadCatalog with definitions, skills and teams arrays')
  const personas = providedFixtures?.personas || squadCatalog.definitions.map(row => ({ id: row.short_id || row.definition_id,
    name: row.name, summary: row.summary, icon: row.icon, source: row.source,
    teams: row.teams.map(team => team.id.replace(/^clawdline\.team\./, '')), suggested_kinds: ['issue', 'feature'] }))
  const requests = []
  // State belongs to a browser cookie, so reviewers do not change each other's scenario.
  const handler = (kind) => async (request, response) => {
    try {
      const url = new URL(request.url, 'http://fixture.invalid')
      const path = url.pathname
      const cookies = Object.fromEntries((request.headers.cookie || '').split(';').map(x => x.trim().split('=')))
      const queryState = url.searchParams.get('review_state')
      if (queryState && !REVIEW_STATES.includes(queryState)) return json(response, 400, { error: 'invalid_review_state' })
      const state = queryState || (REVIEW_STATES.includes(cookies.review_state) ? cookies.review_state : 'ready')
      if (queryState) response.setHeader('set-cookie', [`review_state=${queryState}; Path=/; SameSite=Strict`, 'review_overview_seen=0; Path=/; SameSite=Strict'])
      const tag = REVIEW_LOCALES.includes(url.searchParams.get('lang')) ? url.searchParams.get('lang') : 'en'
      if (path === '/__review/state') {
        const next = url.searchParams.get('state')
        if (!REVIEW_STATES.includes(next)) return json(response, 400, { error: 'invalid_review_state', allowed: REVIEW_STATES })
        response.setHeader('set-cookie', [`review_state=${next}; Path=/; SameSite=Strict`, 'review_overview_seen=0; Path=/; SameSite=Strict'])
        return json(response, 200, { state: next, synthetic: true })
      }
      if (path === '/__review/requests') return json(response, 200, { requests: url.searchParams.has('actor') ? requests.filter(row => row.actor === url.searchParams.get('actor')) : requests })
      if (path === '/__review/manifest') return json(response, 200, manifest)
      if (path.startsWith('/v1/') || path.startsWith('/su/')) {
        const record = { kind, method: request.method, path, state, actor: cookies.review_actor || '', status: null }
        requests.push(record)
        response.once('finish', () => { record.status = response.statusCode })
        if (path === '/v1/strings') return json(response, 200, JSON.parse(await readFile(join(servedOut, kind, 'catalogs', tag + '.json'), 'utf8')))
        if (path === '/v1/auth/session') {
          if (state === 'signed-out' || request.method === 'GET') return json(response, 401, { error: { code: 'not_signed_in' } })
          if (state === 'failed') return json(response, 503, { error: { code: 'fixture_session_failed', message: 'Synthetic API refusal' } })
          return json(response, 201, { account_id: 'review-account', device_id: 'review-viewer', caps: ['read', 'send_prompt'] })
        }
        if (request.method !== 'GET') return json(response, 405, { error: { code: 'review_mutation_unsupported', message: 'This fixture does not persist product mutations.' } })
        if (path === '/v1/devices') return json(response, 200, { devices: [{ id: 'review-viewer', caps: ['read', 'send_prompt'] }] })
        if (path === '/su/v1/operations/overview') {
          if (state === 'forbidden') return json(response, 404, { error: { code: 'not_found' } })
          if (state === 'failed' || (state === 'stale' && cookies.review_overview_seen === '1')) return json(response, 503, { error: { code: 'fixture_overview_failed' } })
          response.setHeader('set-cookie', 'review_overview_seen=1; Path=/; SameSite=Strict')
          return json(response, 200, { schema: 'clawdline.operations.overview.v1', ...demoOverview(new Date('2026-01-15T12:00:00Z')) })
        }
        if (path === '/su/v1/promo/campaigns') return json(response, 200, { total: 0, offset: 0, rows: [] })
        if (state === 'offline') return json(response, 503, { error: { code: 'fixture_offline', message: 'Synthetic machine offline' } })
        if (path === '/v1/health') return json(response, 200, { ok: true, authed: true, api_level: 999, version: 'synthetic-review', platform: 'linux' })
        if (state === 'failed' && !path.includes('/auth/')) return json(response, 503, { error: { code: 'fixture_unavailable', message: 'Synthetic read failure' } })
        if (path === '/v1/sessions') return json(response, 200, snapshot(state))
        if (path === '/v1/events') {
          response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
          response.write('event: sessions\ndata: ' + JSON.stringify(snapshot(state)) + '\n\n')
          return
        }
        if (path === '/v1/transcript') return json(response, 200, { entries: [
          { role: 'user', text: 'Authored sample: prepare the release checklist.' },
          { role: 'tool', tool: 'update_plan', plan: [{ step: 'Inspect the sample project', status: 'completed' }, { step: 'Verify the interface', status: 'inProgress' }, { step: 'Write the review', status: 'pending' }] },
          { role: 'assistant', text: 'Authored sample: the checklist is ready for review.' },
        ], evidence: 'process', id: REVIEW_SESSION, signature: 'synthetic-review' })
        if (path === `/v1/sessions/${REVIEW_SESSION}/info`) return json(response, 200, { info: { session: { ...syntheticRow(), title: syntheticRow().label }, models: [], deploy: [], links: [] } })
        if (path === '/v1/places') return json(response, 200, { at: Date.now(), assistants: [{ id: 'claude', label: 'Claude Code', availability: 'available' }], places: state === 'empty' ? [] : [project] })
        if (path === '/v1/settings/default-models') return json(response, 200, { codex_default_model: '', claude_default_model: '', models: { codex: [], claude: [] } })
        if (['/v1/settings', '/v1/settings/browser', '/v1/settings/notifications'].includes(path)) return json(response, 200, { language: 'auto', voice_language: 'auto', hotkey: 'ctrl+space', console_enabled: true, codex_default_model: '', claude_default_model: '', codex_default_effort: '' })
        if (path === '/v1/voice/language') return json(response, 200, { setting: 'auto', code: 'en', script: '', source: 'language', tag: 'en', script_source: '', script_tag: '' })
        if (path === '/v1/personas') return json(response, 200, { license: 'MIT', personas })
        if (path === '/v1/squad/catalog') return json(response, 200, squadCatalog)
        if (path === '/v1/squad/settings') return json(response, 200, reviewSquadSettings(squadCatalog, url.searchParams.has('place_id') ? 'review-project' : 'global'))
        if (path === '/v1/squad/scopes') return json(response, 200, { scopes: [{ place_id: project.id, scope_id: project.id, kind: 'repo', label: project.label, path: project.path }], scope_ids: [project.id] })
        if (path === '/v1/squad/events/head') return json(response, 200, { seq: 0 })
        if (path === '/v1/squad/events') return json(response, 200, { events: [], next_after: 0, has_more: false })
        if (path === '/v1/squad/session-bindings') return json(response, 200, { bindings: [] })
        if (path === '/v1/squad/skill-sources') return json(response, 200, { skills: [] })
        if (path.startsWith('/v1/work/v2/session-todos/')) return json(response, 200, { ok: true, assigned_items: [], recent_items: [], direct_todos: [], open_decisions: [], truncated: false })
        if (path.startsWith('/v1/work/v2/human-interventions/')) return json(response, 200, { ok: true, rows: [], pruned_resolved: 0 })
        if (path === '/v1/snippets') return json(response, 200, { snippets: state === 'empty' ? [] : [{ id: 'review-workflow', title: 'Authored sample workflow', body: 'Read the sample project and describe the next step.', scope: 'workflow', position: 0, created_at: 1, updated_at: 1 }] })
        if (path === `/v1/sessions/${REVIEW_SESSION}/documents`) return json(response, 200, { documents: [{ scope: 'project', path: 'README.md', bytes: 64, modified: 1 }] })
        if (path === `/v1/sessions/${REVIEW_SESSION}/documents/project/README.md`) {
          response.writeHead(200, { 'content-type': 'text/markdown; charset=utf-8' })
          return response.end('# Authored sample\n\nThis content is intentionally not translated.\n')
        }
        if (path === '/v1/work/v2/items') return json(response, 200, { items: [], rows: [], truncated: false })
        if (path === '/v1/work/v2/proposals') return json(response, 200, { rows: [], truncated: false })
        if (path === '/v1/work/decisions') return json(response, 200, { counts: {}, rows: [], next_cursor: null })
        if (path === '/v1/orchestrator/tasks') return json(response, 200, { at: Date.now(), tasks: [] })
        if (path === '/v1/orchestrator/schedules') return json(response, 200, { schedules: [], tasks: [] })
        if (path === '/v1/projects/review-project/tree') return json(response, 200, { directory: '', entries: [{ name: 'README.md', path: 'README.md', kind: 'file', size: 64 }], truncated: false })
        if (path === '/v1/projects/review-project/tree/file') return json(response, 200, { path: 'README.md', text: '# Authored sample\n\nThis content is intentionally not translated.\n', size: 64, version: 'synthetic-1' })
        if (path === '/v1/projects/review-project/files') return json(response, 200, { files: [], skipped: [], truncated: false })
        // Unknown routes are visible evidence, never silently successful fixtures.
        return json(response, 404, { error: { code: 'review_route_unsupported', message: path } })
      }
      // Unstamped hosted development builds resolve catalogs against the deep
      // document URL. Serve that real catalog file, not the SPA document.
      const documentPath = path.startsWith('/admin/catalogs/') ? path.slice('/admin'.length)
        : path === '/' || path === '/admin' || path.startsWith('/admin/') ? '/index.html' : path
      const directory = join(servedOut, kind)
      const file = resolve(directory, '.' + documentPath)
      if (relative(directory, file).startsWith('..')) return json(response, 403, { error: 'outside_fixture' })
      let content = await readFile(file)
      if (extname(file) === '.html') content = Buffer.from(content.toString().replace('<!-- clawdline:strings -->', '').replace('<!-- clawdline:cloud -->', ''))
      response.writeHead(200, { 'content-type': mime[extname(file)] || 'application/octet-stream', 'cache-control': 'no-store' })
      response.end(content)
    } catch (error) {
      json(response, error.code === 'ENOENT' ? 404 : 500, { error: 'fixture_error', detail: String(error) })
    }
  }
  const local = httpServer(handler('app'))
  const hosted = httpsServer({ key: await readFile(keyFile), cert: await readFile(certFile) }, handler('cloud'))
  const reusePort = key => {
    if (!reused) return 0
    const url = new URL(reused[key])
    if (url.hostname !== '127.0.0.1') throw new Error('Refusing non-loopback reused origin')
    return Number(url.port)
  }
  const listen = (server, port) => new Promise((yes, no) => { server.once('error', no); server.listen(port, '127.0.0.1', yes) })
  await listen(local, reusePort('appOrigin'))
  await listen(hosted, reusePort('cloudOrigin'))
  const appOrigin = `http://127.0.0.1:${local.address().port}`
  const cloudOrigin = `https://127.0.0.1:${hosted.address().port}`
  const manifest = { version: 1, synthetic: true, pid: process.pid, repo, candidateCommit, out: servedOut, appOrigin, cloudOrigin,
    roleMetadata: options.builtins ? 'candidate public display JSON and raw Markdown; synthetic icons, versions, team and role-skill relationships' : providedFixtures ? 'provided-json' : 'authored-sample; not built-in translation acceptance',
    fixtureDigest: createHash('sha256').update(JSON.stringify({ squadCatalog, personas })).digest('hex'),
    states: REVIEW_STATES, locales: REVIEW_LOCALES, createdAt: new Date().toISOString(), catalogHashes: {},
    urls: { sessions: appOrigin + '/', detail: appOrigin + '/#session=review-session', settings: appOrigin + '/#page=settings',
      machineSettings: appOrigin + '/settings.html', workflow: appOrigin + '/#page=workflow', board: appOrigin + '/#page=work',
      projects: appOrigin + '/#page=projects', notifications: appOrigin + '/#page=notifications', plan: appOrigin + '/#page=plan',
      squad: appOrigin + '/#page=squad',
      document: appOrigin + '/#document=1&machine=this-mac&session=review-session&scope=project&path=README.md',
      signIn: cloudOrigin + '/?review_state=signed-out', pairing: cloudOrigin + '/?review_state=pairing',
      admin: cloudOrigin + '/admin?review_state=ready', accounts: cloudOrigin + '/admin/accounts?review_state=ready',
      promotions: cloudOrigin + '/admin/promotions?review_state=ready', usage: cloudOrigin + '/admin/usage?review_state=ready',
      adminForbidden: cloudOrigin + '/admin?review_state=forbidden', adminFailed: cloudOrigin + '/admin?review_state=failed' },
    unsupported: ['Cloud machine selection and shared console: encrypted relay peer not implemented; no production security checks bypassed.',
      'Unknown API routes return review_route_unsupported; inspect /__review/requests and report affected loaded-data surfaces.'] }
  const stop = () => { local.closeAllConnections(); hosted.closeAllConnections(); local.close(); hosted.close() }
  process.once('SIGTERM', () => { stop(); process.exit(0) })
  process.once('SIGINT', () => { stop(); process.exit(0) })
  try {
    if (!reused) await run(join(repo, 'tools/heavy.sh'), [process.execPath, self, '--build', '--repo', repo, '--out', out, '--cloud-origin', cloudOrigin], { env: { ...process.env, HEAVY_HANDOFF: '0' } })
    if (!reused && (execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim() !== candidateCommit || execFileSync('git', ['-C', repo, 'status', '--porcelain'], { encoding: 'utf8' }).trim())) throw new Error('Candidate changed during the build; no review manifest published')
    if (!reused && servedOut !== out) {
      await mkdir(servedOut, { recursive: true })
      for (const kind of ['app', 'cloud']) await cp(join(out, kind), join(servedOut, kind), { recursive: true })
      for (const name of ['loopback-key.pem', 'loopback-cert.pem']) await cp(join(out, name), join(servedOut, name))
    }
    await writeFile(join(servedOut, 'fixture-data.json'), JSON.stringify({ squadCatalog, personas }, null, 2) + '\n')
    for (const tag of REVIEW_LOCALES) {
      const hash = createHash('sha256').update(await readFile(join(servedOut, 'app/catalogs', tag + '.json'))).digest('hex')
      if (reused && hash !== reused.catalogHashes[tag]) throw new Error('Published catalog changed since its build manifest')
      manifest.catalogHashes[tag] = hash
    }
    await writeFile(manifestFile, JSON.stringify(manifest, null, 2) + '\n')
    console.log(JSON.stringify(manifest))
    return { manifest, stop }
  } catch (error) { stop(); throw error }
}

if (process.argv[1] && resolve(process.argv[1]) === self) {
  const args = reviewArgs(process.argv.slice(2))
  if (args.help || (!args.serve && !args.build)) console.log('node localization-review.mjs --repo REPO --serve --out TASK_OUTPUT --manifest TASK_MANIFEST\nStop the exact manifest pid with SIGTERM. Builds acquire tools/heavy.sh; servers do not hold its lease.')
  else if (!args.repo || !args.out) throw new Error('--repo and --out are required')
  else if (args.build) await buildReview({ repo: resolve(args.repo), out: resolve(args.out), cloudOrigin: args['cloud-origin'] })
  else if (!args.manifest) throw new Error('--manifest is required')
  else await serveReview(args)
}

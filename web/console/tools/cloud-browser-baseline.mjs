#!/usr/bin/env node
// Run through tools/heavy.sh: no browser automation and no production daemon.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';
import { mkdirSync, mkdtempSync, rmSync, existsSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { spawnSync, spawn } from 'node:child_process';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
const [action = 'help', outputArgument] = process.argv.slice(2);
if (!['build', 'start', 'summarize'].includes(action) || !outputArgument) { console.log('tools/heavy.sh node web/console/tools/cloud-browser-baseline.mjs build|start|summarize <absolute task artifacts directory>\nBuild first. Start prints loopback URL and child PID. POST /stop with matching Origin to stop; no server survives this process.'); process.exit(action === 'help' ? 0 : 2); }
const output = resolve(outputArgument), bundle = resolve(output, 'entry.js'), binary = resolve(output, 'browser-fixture.test'); mkdirSync(output, { recursive: true });
if (action === 'summarize') {
 const rounds = [1, 2, 3].map(n => JSON.parse(readFileSync(resolve(output, `round-${n}.json`), 'utf8')));
 if (new Set(rounds.map(r => [r.server?.source_sha256, r.server?.binary_sha256, r.server?.bundle_sha256].join(':'))).size !== 1) throw new Error('Round source/binary/bundle identities differ');
 if (rounds.some(r => r.browser?.kind !== 'native-browser' || r.browser?.pass !== true)) throw new Error('Three passing native-browser rounds are required; inspect each raw failure');
 const shape = { idle: [0, 0], 'active-one': ['viewers', 0], 'multi-session': ['twice', 0], 'multi-viewer': ['viewers', 0], 'duplicate-rejected': [0, 1], 'receiver-pin-revoked': [0, 1], 'receiver-pin-restored': [1, 0], 'fixture-revocation': [0, 1], 'fixture-restored': [1, 0], 'socket-reconnect': ['viewers', 0] };
 for (const round of rounds) {
  if (round.browser.scenarios.length !== 31) throw new Error('Expected the complete 31-scenario native matrix');
  for (const viewers of [1,2,4]) for (const [prefix, [delivered, errors]] of Object.entries(shape)) {
   const row = round.browser.scenarios.find(s => s.name === `${prefix}-${viewers}`), expected = delivered === 'viewers' ? viewers : delivered === 'twice' ? viewers * 2 : delivered;
   if (!row?.pass || row.delivered !== expected || row.errors !== errors) throw new Error(`Wrong canary/error count for ${prefix}-${viewers}`);
   if (['active-one','multi-session','multi-viewer','socket-reconnect'].includes(prefix) && new Set(row.proofs.map(p => p.context)).size !== viewers) throw new Error(`Independent context proof missing for ${prefix}-${viewers}`);
  }
  const native = round.browser.scenarios.find(s => s.name === 'native-direct-auto-fallback');
  if (!native?.pass || !native.native_rtc || !native.same_client_fallback || !native.direct_then_relay || native.verified_checkpoints !== 2 || native.delivered < 2) throw new Error('Native same-session direct/fallback proof missing');
 }
 const distribution = numbers => { const sorted = [...numbers].sort((a,b) => a-b); return { n: sorted.length, median: sorted[Math.floor(sorted.length / 2)], range: [sorted[0], sorted.at(-1)] }; };
 const summary = { kind: 'native-browser', rounds: 3, browser_cpu: null, cpu_subject: 'combined fixture process only', scenarios: rounds[0].browser.scenarios.map(first => { const rows = rounds.map(r => r.browser.scenarios.find(s => s.name === first.name)); if (rows.some(r => !r)) throw new Error('Scenario matrix differs'); return { name: first.name, viewers: first.viewers, reads: first.reads, delivered: rows.map(r => r.delivered), errors: rows.map(r => r.errors), duration_ms: distribution(rows.map(r => r.duration_ms)), fixture_cpu_ns: distribution(rows.map(r => r.fixture_after.process_user_cpu_ns + r.fixture_after.process_system_cpu_ns - r.fixture_before.process_user_cpu_ns - r.fixture_before.process_system_cpu_ns)), counter_deltas: rows.map(r => Object.fromEntries([...new Set([...Object.keys(r.fixture_before.counters), ...Object.keys(r.fixture_after.counters)])].map(key => [key, (r.fixture_after.counters[key] || 0) - (r.fixture_before.counters[key] || 0)]))) }; }) };
 writeFileSync(resolve(output, 'summary.json'), JSON.stringify(summary, null, 2)); console.log(JSON.stringify(summary, null, 2));
} else if (action === 'build') {
 const deps = spawnSync(process.execPath, ['tools/ensure-web-deps.mjs'], { cwd: root, stdio: 'inherit' }); if (deps.status !== 0) process.exit(deps.status || 1);
 const require = createRequire(resolve(root, 'web/console/package.json')); const { build } = require('esbuild');
 const built = await build({ absWorkingDir: root, entryPoints: [resolve(root, 'web/console/tools/cloud-browser-baseline-entry.mjs')], bundle: true, format: 'esm', platform: 'browser', outfile: bundle, metafile: true, define: { 'import.meta.env.DEV': 'false' } });
 writeFileSync(resolve(output, 'bundle-inputs.json'), JSON.stringify(built.metafile.inputs, null, 2));
 const sourceManifest = {}; for (const path of [...Object.keys(built.metafile.inputs), 'internal/domain/persona/catalog/performance.md', 'internal/transport/cloud/browser_efficiency_fixture_test.go', 'web/console/tools/cloud-browser-baseline.mjs', 'internal/transport/cloud/direct.go', 'internal/transport/cloud/terminal.go', 'internal/transport/cloud/link.go', 'internal/adapters/cloud/transport.go', 'internal/domain/cloud/envelope.go', 'internal/domain/cloud/keys.go']) sourceManifest[path] = createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex');
 writeFileSync(resolve(output, 'source-manifest.json'), JSON.stringify(sourceManifest, null, 2));
 const work = resolve(output, '../work'); mkdirSync(work, { recursive: true }); const cache = mkdtempSync(resolve(work, 'browser-fixture-cache-'));
 let exitCode = 0;
 try {
 const go = spawnSync('go', ['test', '-c', '-o', binary, './internal/transport/cloud'], { cwd: root, stdio: 'inherit', env: { ...process.env, GOCACHE: cache } }); if (go.status !== 0) throw new Error(`Go fixture build failed: ${go.status}`);
 const digest = path => createHash('sha256').update(readFileSync(path)).digest('hex');
 writeFileSync(resolve(output, 'hashes.json'), JSON.stringify({ binary: digest(binary), bundle: digest(bundle), performance_method: digest(resolve(root, 'internal/domain/persona/catalog/performance.md')) }, null, 2));
 const smoke = spawnSync(binary, ['-test.run=^TestBrowserEfficiencyFixtureSmoke$', '-test.v', '-test.timeout=45s'], { cwd: root, encoding: 'utf8' });
 writeFileSync(resolve(output, 'smoke.log'), (smoke.stdout || '') + (smoke.stderr || '')); process.stdout.write(smoke.stdout || ''); process.stderr.write(smoke.stderr || ''); if (smoke.status !== 0) throw new Error(`Go peer smoke failed: ${smoke.status}`);
 await startupSmoke();
 const gate = spawnSync('tools/check.sh', [], { cwd: root, encoding: 'utf8', env: { ...process.env, GOCACHE: cache, HEAVY_HANDOFF: '0', CLAWDLINE_BROWSER_FIXTURE: '0' } });
 writeFileSync(resolve(output, 'check-gate.log'), (gate.stdout || '') + (gate.stderr || '')); process.stdout.write(gate.stdout || ''); process.stderr.write(gate.stderr || ''); if (gate.status !== 0) throw new Error(`Scoped local gate failed: ${gate.status}`);
 console.log(`Built ${binary} and ${bundle}; Go peer smoke and scoped local gate passed (not browser evidence)`);
 } catch (error) { console.error(error.message); exitCode = 1; }
 finally { rmSync(cache, { recursive: true, force: true }); writeFileSync(resolve(output, 'cache-cleanup.json'), JSON.stringify({ task_owned_cache: cache, removed: !existsSync(cache) }, null, 2)); }
 if (exitCode) process.exit(exitCode);
} else {
 const hashes = JSON.parse(readFileSync(resolve(output, 'hashes.json'), 'utf8'));
 for (const [path, hash] of [[binary, hashes.binary], [bundle, hashes.bundle]]) if (createHash('sha256').update(readFileSync(path)).digest('hex') !== hash) throw new Error('Fixture binary/bundle hash differs; rebuild first');
 const expected = JSON.parse(readFileSync(resolve(output, 'source-manifest.json'), 'utf8'));
 for (const [path, hash] of Object.entries(expected)) if (createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex') !== hash) throw new Error(`Source changed since fixture build: ${path}; rebuild first`);
 const child = spawn(binary, ['-test.run=^TestBrowserEfficiencyFixture$', '-test.v', '-test.timeout=30m'], { cwd: root, stdio: 'inherit', env: { ...process.env, CLAWDLINE_BROWSER_SOURCE_ROOT: root, CLAWDLINE_BROWSER_FIXTURE: '1', CLAWDLINE_BROWSER_OUTPUT: process.env.CLAWDLINE_BROWSER_RESULTS || output, CLAWDLINE_BROWSER_BUNDLE: bundle } });
 for (const signal of ['SIGTERM','SIGINT']) process.on(signal, () => child.kill(signal));
 child.on('exit', (code, signal) => process.exit(code ?? (signal ? 1 : 0)));
}

async function startupSmoke() {
 const child = spawn(binary, ['-test.run=^TestBrowserEfficiencyFixture$', '-test.v', '-test.timeout=20s'], { cwd: root, env: { ...process.env, CLAWDLINE_BROWSER_SOURCE_ROOT: root, CLAWDLINE_BROWSER_FIXTURE: '1', CLAWDLINE_BROWSER_OUTPUT: resolve(output, 'startup-smoke'), CLAWDLINE_BROWSER_BUNDLE: bundle }, stdio: ['ignore', 'pipe', 'pipe'] });
 let log = '', stderr = '', readyResolve, readyReject, timer;
 const ready = new Promise((resolve, reject) => { readyResolve = resolve; readyReject = reject; });
 const exited = new Promise(resolve => { child.on('exit', (code, signal) => { readyReject(new Error(`fixture exited before ready: ${code ?? signal}`)); resolve({ code, signal }); }); child.on('error', error => { readyReject(error); resolve({ code: -1 }); }); });
 child.stdout.on('data', chunk => { log += chunk; const match = log.match(/BROWSER_FIXTURE_READY url=(http:\/\/127\.0\.0\.1:\d+) pid=(\d+)/); if (match) readyResolve({ url: match[1], pid: Number(match[2]) }); });
 child.stderr.on('data', chunk => { stderr += chunk; });
 try {
  const state = await Promise.race([ready, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('fixture ready timeout')), 10000); })]); clearTimeout(timer);
  const page = await fetch(state.url); if (page.status !== 200 || !(await page.text()).includes('Start round')) throw new Error('fixture page unavailable');
  const entry = await fetch(`${state.url}/entry.js`); if (entry.status !== 200 || !(await entry.text()).includes('CloudClient')) throw new Error('fixture production bundle unavailable');
  const unknown = await fetch(`${state.url}/repo/AGENTS.md`); if (unknown.status !== 404) throw new Error('fixture exposed repository'); await unknown.text();
  const refused = await fetch(`${state.url}/collect`, { method: 'POST', headers: { Origin: state.url, 'Content-Type': 'application/json' }, body: JSON.stringify({ kind: 'native-browser', round: 1, unknown: 'synthetic-negative' }) }); if (refused.status !== 400) throw new Error('collector schema negative failed'); await refused.text();
  const stop = await fetch(`${state.url}/stop`, { method: 'POST', headers: { Origin: state.url } }); if (stop.status !== 200) throw new Error('fixture stop failed'); await stop.text();
  const exit = await exited; if (exit.code !== 0) throw new Error('fixture exited unsuccessfully');
  let stopped = false; try { await fetch(state.url); } catch { stopped = true; } if (!stopped) throw new Error('fixture still listening after normal stop');
  writeFileSync(resolve(output, 'startup-smoke.json'), JSON.stringify({ kind: 'node-http-smoke-not-browser', ...state, page: 200, bundle: 200, repository: 404, schema_negative: 400, exited: exit.code, stopped }, null, 2));
 } finally { clearTimeout(timer); if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM'); await exited; writeFileSync(resolve(output, 'startup-smoke.log'), log + stderr); }
}

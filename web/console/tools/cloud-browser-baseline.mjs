#!/usr/bin/env node
// Run through tools/heavy.sh: no browser automation and no production daemon.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';
import { mkdirSync, mkdtempSync, rmSync, existsSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { spawnSync, spawn } from 'node:child_process';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
const [action = 'help', outputArgument, buildArgument] = process.argv.slice(2);
if (!['build', 'start', 'summarize'].includes(action) || !outputArgument) { console.log('tools/heavy.sh node web/console/tools/cloud-browser-baseline.mjs build|start|summarize <absolute task artifacts directory>\nSummarize also requires the exact build artifacts directory as its third argument. Build first. Start prints loopback URL and child PID. POST /stop with matching Origin to stop; no server survives this process.'); process.exit(action === 'help' ? 0 : 2); }
const output = resolve(outputArgument), bundle = resolve(output, 'entry.js'), binary = resolve(output, 'browser-fixture.test'); mkdirSync(output, { recursive: true });
if (action === 'summarize') {
 const rounds = [1, 2, 3].map(n => JSON.parse(readFileSync(resolve(output, `round-${n}.json`), 'utf8')));
 if (!buildArgument) throw new Error('Summarize requires the exact build artifacts directory as its third argument');
 const buildOutput = resolve(buildArgument);
 const identity = JSON.parse(readFileSync(resolve(buildOutput, 'hashes.json'), 'utf8'));
 const manifest = JSON.parse(readFileSync(resolve(buildOutput, 'source-manifest.json'), 'utf8'));
 const hash = bytes => createHash('sha256').update(bytes).digest('hex');
 const requireHash = value => { if (typeof value !== 'string' || !/^[a-f0-9]{64}$/.test(value)) throw new Error('Missing artifact identity'); return value; };
 for (const [name, path] of [['binary', 'browser-fixture.test'], ['bundle', 'entry.js']]) if (hash(readFileSync(resolve(buildOutput, path))) !== requireHash(identity[name])) throw new Error('Build artifact identity differs');
 requireHash(identity.source);
 if (!manifest || Object.keys(manifest).length === 0) throw new Error('Missing source manifest');
 for (const [path, expected] of Object.entries(manifest)) if (hash(readFileSync(resolve(root, path))) !== requireHash(expected)) throw new Error(`Source manifest differs: ${path}`);
 if (sourceDigest() !== identity.source) throw new Error('Source aggregate differs');
 const nonnegative = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
 const integer = value => nonnegative(value) && Number.isSafeInteger(value);
 const counterKeys = new Set(['accepted_commands','acks','admission_refused','connections','direct_frames_delivered','direct_native_requests','direct_peer_drops','http_requests','injected_duplicates','logical_reads','machine_publications','native_direct_offers','relay_delivered','subscribe','terminal_frame_observed','terminal_relay_frames_delivered','terminal_requests','unsubscribe','viewer_publish_frames','ws_in_frames','ws_in_json_bytes','ws_out_frames','ws_out_json_bytes']);
 const sample = value => {
  if (!value || !integer(value.process_user_cpu_ns) || !integer(value.process_system_cpu_ns) || !value.counters || typeof value.counters !== 'object' || Array.isArray(value.counters)) throw new Error('Invalid CPU/counter sample');
  for (const [key, count] of Object.entries(value.counters)) if (!counterKeys.has(key) || !integer(count)) throw new Error('Invalid counter');
 };
 const monotonic = (before, after) => {
  sample(before); sample(after);
  if (after.process_user_cpu_ns < before.process_user_cpu_ns || after.process_system_cpu_ns < before.process_system_cpu_ns) throw new Error('CPU sample regressed');
  for (const [key, count] of Object.entries(before.counters)) if (!Object.hasOwn(after.counters,key) || after.counters[key] < count) throw new Error('Counter sample regressed or disappeared');
 };
 const shape = { idle: [0, 0], 'active-one': ['viewers', 0], 'multi-session': ['twice', 0], 'multi-viewer': ['viewers', 0], 'duplicate-rejected': [0, 1], 'receiver-pin-revoked': [0, 1], 'receiver-pin-restored': [1, 0], 'fixture-revocation': [0, 1], 'fixture-restored': [1, 0], 'socket-reconnect': ['viewers', 0] };
 let previous;
 for (const [index, round] of rounds.entries()) {
  if (round.browser?.kind !== 'native-browser' || round.browser.pass !== true || round.browser.failure !== '' || round.browser.round !== index + 1) throw new Error('Distinct numbered passing native-browser rounds are required');
  for (const [field, expected] of [['source_sha256',identity.source],['binary_sha256',identity.binary],['bundle_sha256',identity.bundle]]) if (requireHash(round.server?.[field]) !== expected) throw new Error('Round artifact identity differs from build');
  const rows = round.browser.scenarios;
  if (!Array.isArray(rows) || rows.length !== 31 || new Set(rows.map(s => s.name)).size !== 31) throw new Error('Expected the complete unique 31-scenario native matrix');
  for (const row of rows) {
   if (row.pass !== true || !nonnegative(row.duration_ms) || !(row.memory_bytes === null || integer(row.memory_bytes)) || !Array.isArray(row.proofs) || !integer(row.delivered) || !integer(row.errors) || !integer(row.verified_checkpoints)) throw new Error('Invalid scenario sample');
   monotonic(row.fixture_before,row.fixture_after);
   if (previous) monotonic(previous,row.fixture_before);
   previous = row.fixture_after;
  }
  for (const viewers of [1,2,4]) for (const [prefix, [delivered, errors]] of Object.entries(shape)) {
   const row = rows.find(s => s.name === `${prefix}-${viewers}`), expected = delivered === 'viewers' ? viewers : delivered === 'twice' ? viewers * 2 : delivered;
   const reads = prefix === 'multi-session' ? viewers * 2 : ['active-one','socket-reconnect'].includes(prefix) ? viewers : 0;
   const contexts = ['idle','active-one','multi-session','multi-viewer','socket-reconnect'].includes(prefix) ? viewers : 1;
   if (!row || row.viewers !== contexts || row.reads !== reads || row.delivered !== expected || row.errors !== errors || row.verified_checkpoints !== 0 || row.native_rtc !== false || row.same_client_fallback !== false || row.direct_then_relay !== false || row.proofs.length !== expected) throw new Error(`Wrong scenario shape for ${prefix}-${viewers}`);
   const actualReads = (row.fixture_after.counters.logical_reads ?? 0) - (row.fixture_before.counters.logical_reads ?? 0);
   if (actualReads !== reads) throw new Error('Logical read counter differs');
   const sequences = new Set();
   for (const proof of row.proofs) {
    if (typeof proof.context !== 'string' || !proof.context.startsWith('synthetic-') || !Number.isSafeInteger(proof.sequence) || proof.sequence <= 0 || sequences.has(proof.sequence)) throw new Error('Missing or duplicate proof sequence/context');
    sequences.add(proof.sequence);
    const sessions = prefix === 'socket-reconnect' ? ['two'] : prefix === 'multi-session' ? ['one','two'] : ['one'];
    if (!sessions.includes(proof.session) || typeof proof.canary !== 'string' || !proof.canary.startsWith('canary-')) throw new Error('Missing proof content/session');
    if (reads && proof.canary !== `canary-screen-${proof.session}`) throw new Error('Read canary differs');
   }
   if (expected && new Set(row.proofs.map(p => p.context)).size !== contexts) throw new Error('Independent context proof missing');
   if (prefix === 'multi-session') for (const context of new Set(row.proofs.map(p => p.context))) if (['one','two'].some(session => row.proofs.filter(p => p.context === context && p.session === session).length !== 1)) throw new Error('Missing per-context session proof');
  }
  const native = rows.find(s => s.name === 'native-direct-auto-fallback');
  if (!native?.pass || native.viewers !== 1 || native.reads !== 0 || native.errors !== 0 || native.native_rtc !== true || native.same_client_fallback !== true || native.direct_then_relay !== true || native.verified_checkpoints !== 2 || native.delivered < 2 || native.proofs.length !== 1 || typeof native.proofs[0].context !== 'string' || !native.proofs[0].context.startsWith('synthetic-') || native.proofs[0].session !== 'terminal' || native.proofs[0].canary !== 'canary-native-relay') throw new Error('Native same-session direct/fallback proof missing');
  monotonic(previous,round.server); previous = round.server;
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
 writeFileSync(resolve(output, 'hashes.json'), JSON.stringify({ binary: digest(binary), bundle: digest(bundle), performance_method: digest(resolve(root, 'internal/domain/persona/catalog/performance.md')), source: sourceDigest() }, null, 2));
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

function sourceDigest() {
 const h = createHash('sha256');
 for (const path of ['internal/domain/persona/catalog/performance.md','internal/transport/cloud/browser_efficiency_fixture_test.go','web/console/tools/cloud-browser-baseline-entry.mjs','web/console/tools/cloud-browser-baseline.mjs','web/console/src/legacy/js/net/cloud-client.js','web/console/src/legacy/js/net/cloud-crypto.js']) { h.update(path); h.update(readFileSync(resolve(root,path))); }
 return h.digest('hex');
}

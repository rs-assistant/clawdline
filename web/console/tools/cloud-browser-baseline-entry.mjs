// Production modules bundled unchanged; this file orchestrates synthetic measurements only.
import { CloudClient } from '../src/legacy/js/net/cloud-client.js';
import { importMasterSecret, importSenderPublicKey, bytesBase64 } from '../src/legacy/js/net/cloud-crypto.js';
import { TerminalChannelTransport } from '../src/cloud/terminal-transport.ts';
import { CloudTerminalSession } from '../src/cloud/terminal-session.ts';
const post = async (path, body) => { const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); if (!r.ok) throw new Error(`fixture HTTP ${r.status}`); return r.json(); };
const deadline = (promise, label) => { let timer; return Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`fixture timeout: ${label}`)), 12000); })]).finally(() => clearTimeout(timer)); };
const nonce = new URL(location.href).searchParams.get('nonce');
const control = body => post('/control', body);
const sampleFixture = async () => { const r = await fetch('/stats'); if (!r.ok) throw new Error('fixture stats'); const s = await r.json(); return { counters: s.counters, process_user_cpu_ns: s.process_user_cpu_ns, process_system_cpu_ns: s.process_system_cpu_ns }; };
if (location.pathname === '/frame') {
  document.querySelector('h1').textContent = 'Independent native viewer';
  document.querySelector('#start').remove();
  const id = new URL(location.href).searchParams.get('id');
  let terminalID, terminalSession, terminalTransport;
  const nativePeers = [];
  let client, pairing, sequence = 0, deliveries = [], errors = [], pending = new Map();
  const reply = (request, result, error) => parent.postMessage({ fixture: true, nonce, id, request, result, error }, location.origin);
  const waitEvent = (test, label) => deadline(new Promise(resolve => { const off = client.events(e => { if (test(e)) { off(); resolve(e); } }); }), label);
  const subscribe = async session => { const channel = `t/synthetic-machine/${session}`; if (client.socketSubscriptions.has(channel)) return; const ready = waitEvent(e => e.type === 'subscriptions' && e.channels.includes(channel), 'subscription'); client.subscribe([channel]); await ready; };
  async function execute(action, args) {
    if (action === 'init') {
      if (globalThis.WebSocket === undefined || crypto?.subtle === undefined) throw new Error('native APIs unavailable');
      const signing = await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
      const config = await post('/register', { id, public: bytesBase64(new Uint8Array(await crypto.subtle.exportKey('raw', signing.publicKey))) });
      terminalID = config.terminal;
      pairing = { machineID: config.machine, senderID: config.machine, keyID: 'ms-1', masterKey: await importMasterSecret(config.master), senderKey: await importSenderPublicKey(config.sender) };
      client = new CloudClient({ relayURL: `ws://${location.host}/v1/connect?id=${encodeURIComponent(id)}`, deviceID: id, account: 'synthetic-account', deviceToken: config.token, devicePrivateKey: signing.privateKey, machinePairings: { [config.machine]: pairing }, senderKeys: { [config.machine]: pairing.senderKey }, allowWrites: true,
        // Persist synthetic viewer sequences before publishing, including across socket reconnects.
        nextSequence: async () => { sequence++; sessionStorage.setItem(`fixture-sequence:${id}`, String(sequence)); return sequence; }, BroadcastChannel: null });
      client.events(e => {
        if (e.type === 'read') { deliveries.push({ canary: e.data?.canary, seq: e.envelope.seq, session: e.identity.session }); document.querySelector('#raw').textContent = JSON.stringify(deliveries.at(-1)); const p = pending.get(e.data?.canary); if (p) { pending.delete(e.data.canary); p(e); } }
        if (e.type === 'error') errors.push(e.error?.code || 'unknown');
      });
      const live = waitEvent(e => e.type === 'connection' && e.state === 'live', 'native challenge signing'); await client.start(); await live;
      return { id, nativeWebSocket: client.socket instanceof WebSocket, nonExtractableSigningKey: signing.privateKey.extractable === false };
    }
    if (action === 'read') { const start = performance.now(), before = deliveries.length; const body = await deadline(client.screen({ machine: 'synthetic-machine', session: args.session }), 'screen'); if (body.canary !== `canary-screen-${args.session}` || body.session !== args.session || deliveries.length !== before + 1 || deliveries.at(-1)?.session !== args.session || deliveries.at(-1)?.canary !== body.canary) throw new Error('wrong read content or delivery count'); return { duration_ms: performance.now() - start, delivered: 1, proof: { context: id, canary: body.canary, sequence: deliveries.at(-1).seq, session: args.session } }; }
    if (action === 'publish') {
      await subscribe(args.session); const before = deliveries.length;
      const got = deadline(new Promise(resolve => pending.set(args.canary, resolve)), 'precise canary');
      const published = await control({ action: 'publish', id, ...args }); const event = await got;
      if (event.envelope.seq !== published.seq || event.identity.session !== args.session || deliveries.length !== before + 1) throw new Error('canary sequence/session/count mismatch');
      return { seq: published.seq, delivered: 1, proof: { context: id, canary: event.data.canary, sequence: event.envelope.seq, session: args.session } };
    }
    if (action === 'negative') {
      const before = deliveries.length, errorsBefore = errors.length;
      // The ordered pong is a processing barrier: no fixed sleep is used to infer rejection.
      const rejected = waitEvent(e => e.type === 'error' && e.error?.code === 'replay', 'duplicate replay rejection');
      await control({ action: 'duplicate', id, ...args }); await rejected;
      if (deliveries.length !== before || errors.length !== errorsBefore + 1) throw new Error('duplicate accepted or wrong error count');
      return { delivered: 0, errors: 1 };
    }
    if (action === 'pin-revocation') {
      const machine = 'synthetic-machine', before = deliveries.length;
      client.machinePairings.delete(machine); client.senderKeys.delete(machine);
      const rejected = waitEvent(e => e.type === 'error' && e.error?.code === 'machine_not_paired', 'receiver pin removal');
      try { await control({ action: 'publish', id, session: 'one', canary: 'canary-unpaired-rejected' }); await rejected; if (deliveries.length !== before) throw new Error('unpaired receiver accepted canary'); }
      finally { client.machinePairings.set(machine, pairing); client.senderKeys.set(machine, pairing.senderKey); }
      return { delivered: 0, errors: 1 };
    }
    if (action === 'admission') {
      const before = deliveries.length; await control({ action: 'revoke', id });
      let refused = false; try { await control({ action: 'publish', id, session: 'one', canary: 'canary-revoked' }); } catch (e) { if (e.message === 'fixture HTTP 409') refused = true; else throw e; }
      if (!refused || deliveries.length !== before) throw new Error('fixture admission revocation ineffective');
      await control({ action: 'restore', id }); return { delivered: 0, errors: 1 };
    }
    if (action === 'reconnect') {
      const down = waitEvent(e => e.type === 'connection' && e.state === 'offline', 'socket closed'); await control({ action: 'disconnect', id }); await down;
      const live = waitEvent(e => e.type === 'connection' && e.state === 'live', 'same-client reconnect'); await client.start(); await live; return { sameClient: true };
    }
    if (action === 'idle') { const start = performance.now(); await new Promise(resolve => setTimeout(resolve, 1000)); const pong = waitEvent(e => e.type === 'pong', 'idle barrier'); client._send({ type: 'ping' }); await pong; return { duration_ms: performance.now() - start, delivered: 0 }; }
    if (action === 'direct-fallback') {
      if (!globalThis.RTCPeerConnection) throw new Error('native RTC unavailable');
      terminalTransport = new TerminalChannelTransport(client, 'synthetic-machine', undefined, () => { const peer = new RTCPeerConnection({ iceServers: [] }); nativePeers.push(peer); return peer; });
      terminalSession = new CloudTerminalSession(terminalTransport, `fixture-tab-${id}`);
      const framesObserved = new Set(); let wrongFrame = false; const stopObservation = terminalSession.subscribe(s => { if (s.frame) { framesObserved.add(s.frame); if (!['canary-native-direct', 'canary-native-relay'].includes(s.frame.lines?.[0])) wrongFrame = true; } });
      const state = predicate => { if (predicate(terminalSession.snapshot)) return Promise.resolve(terminalSession.snapshot); return deadline(new Promise(resolve => { const off = terminalSession.subscribe(s => { if (predicate(s)) { off(); resolve(s); } }); }), 'native terminal state'); };
      await terminalSession.start(); await terminalSession.attach(terminalID); await terminalSession.acquire('acquire');
      const direct = await state(s => s.carrier === 'direct' && s.frame?.lines?.[0] === 'canary-native-direct');
      if (!nativePeers.length || nativePeers.some(p => p.getConfiguration().iceServers.length !== 0)) throw new Error('native isolated RTC missing');
      // This cuts only the server's Pion peer. Production session code alone chooses fallback.
      const relay = state(s => s.carrier === 'relay' && s.frame?.lines?.[0] === 'canary-native-relay');
      await control({ action: 'direct-drop', id }); await relay;
      if (wrongFrame || framesObserved.size < 2) throw new Error('native terminal frame content/count mismatch');
      stopObservation(); terminalSession.dispose(); terminalTransport.dispose(); terminalSession = terminalTransport = null;
      for (const peer of nativePeers) peer.close();
      return { delivered: framesObserved.size, verifiedCheckpoints: 2, sameClient: true, nativeRTC: true, directThenRelay: true, proof: { context: id, canary: 'canary-native-relay', session: 'terminal' } };
    }
    if (action === 'stop') { terminalSession?.dispose(); terminalTransport?.dispose(); for (const peer of nativePeers) peer.close(); client.stop(); return {}; }
    throw new Error('unknown fixture action');
  }
  window.addEventListener('message', async e => {
    if (e.origin !== location.origin || e.source !== parent || e.data?.nonce !== nonce || e.data?.id !== id || !e.data?.fixture) return;
    try { reply(e.data.request, await execute(e.data.action, e.data.args || {})); } catch (error) { reply(e.data.request, null, String(error.message).slice(0, 300)); }
  });
  document.querySelector('#status').textContent = id;
  reply('frame-ready', {});
} else {
  let round = 0, serial = 0;
  const pending = new Map(), frames = new Map(), results = [];
  window.addEventListener('message', e => {
    const frame = frames.get(e.data?.id);
    if (e.origin !== location.origin || !frame || e.source !== frame.element.contentWindow || e.data?.nonce !== frame.nonce || !e.data?.fixture) return;
    const key = `${e.data.id}:${e.data.request}`, p = pending.get(key); if (!p) return; pending.delete(key); e.data.error ? p.reject(new Error(e.data.error)) : p.resolve(e.data.result);
  });
  const request = (frame, action, args = {}) => { const request = String(++serial); const promise = new Promise((resolve, reject) => pending.set(`${frame.id}:${request}`, { resolve, reject })); frame.element.contentWindow.postMessage({ fixture: true, id: frame.id, nonce: frame.nonce, request, action, args }, location.origin); return deadline(promise, action); };
  const newFrame = async index => {
    const id = `synthetic-${crypto.randomUUID()}`, frame = { id, nonce: crypto.randomUUID(), element: document.createElement('iframe') };
    frames.set(id, frame); const ready = new Promise((resolve, reject) => pending.set(`${id}:frame-ready`, { resolve, reject }));
    frame.element.src = `/frame?id=${id}&nonce=${frame.nonce}`; frame.element.title = `Independent viewer ${index}`; document.querySelector('#frames').append(frame.element);
    await deadline(ready, 'iframe'); const init = await request(frame, 'init'); if (!init.nativeWebSocket || !init.nonExtractableSigningKey) throw new Error('native client proof missing'); return frame;
  };
  document.querySelector('#start').onclick = async () => {
    if (round >= 3) return;
    const button = document.querySelector('#start'), status = document.querySelector('#status'), raw = document.querySelector('#raw'); button.disabled = true;
    const result = { kind: 'native-browser', round: ++round, pass: false, failure: '', environment: { user_agent: navigator.userAgent, secure_context: isSecureContext, cross_origin_isolated: crossOriginIsolated }, scenarios: [] };
    const run = async (name, viewers, reads, fn) => { status.textContent = `Round ${round}: ${name}`; const before = await sampleFixture(); const start = performance.now(); const values = await fn(); const elapsed = performance.now() - start; const after = await sampleFixture(); const row = { proofs: values.flatMap(v => v?.proof ? [v.proof] : []), native_rtc: values.some(v => v?.nativeRTC === true), same_client_fallback: values.some(v => v?.sameClient === true), direct_then_relay: values.some(v => v?.directThenRelay === true), fixture_before: before, fixture_after: after, name, viewers, reads, delivered: values.reduce((n, v) => n + (v?.delivered || 0), 0), verified_checkpoints: values.reduce((n,v) => n + (v?.verifiedCheckpoints || 0),0), errors: values.reduce((n, v) => n + (v?.errors || 0), 0), duration_ms: elapsed, memory_bytes: performance.memory?.usedJSHeapSize ?? null, pass: true }; result.scenarios.push(row); raw.textContent = JSON.stringify(result, null, 2); return values; };
    try {
      for (const count of [1, 2, 4]) {
        const batch = []; for (let i = 0; i < count; i++) batch.push(await newFrame(i));
        await run(`idle-${count}`, count, 0, () => Promise.all(batch.map(f => request(f, 'idle'))));
        await run(`active-one-${count}`, count, count, () => Promise.all(batch.map(f => request(f, 'read', { session: 'one' }))));
        await run(`multi-session-${count}`, count, count * 2, async () => (await Promise.all(batch.map(async f => [await request(f, 'read', { session: 'one' }), await request(f, 'read', { session: 'two' })]))).flat());
        await run(`multi-viewer-${count}`, count, 0, () => Promise.all(batch.map((f, i) => request(f, 'publish', { session: 'one', canary: `canary-round-${round}-viewers-${count}-${i}` }))));
        const first = batch[0], canary = `canary-negative-${round}-${count}`;
        const positive = await request(first, 'publish', { session: 'one', canary });
        await run(`duplicate-rejected-${count}`, 1, 0, async () => [await request(first, 'negative', { session: 'one', canary, seq: positive.seq })]);
        await run(`receiver-pin-revoked-${count}`, 1, 0, async () => [await request(first, 'pin-revocation')]);
        await run(`receiver-pin-restored-${count}`, 1, 0, async () => [await request(first, 'publish', { session: 'one', canary: `canary-pin-restored-${round}-${count}` })]);
        await run(`fixture-revocation-${count}`, 1, 0, async () => [await request(first, 'admission')]);
        await run(`fixture-restored-${count}`, 1, 0, async () => [await request(first, 'publish', { session: 'one', canary: `canary-restored-${round}-${count}` })]);
        await run(`socket-reconnect-${count}`, count, count, () => Promise.all(batch.map(async f => { await request(f, 'reconnect'); return request(f, 'read', { session: 'two' }); })));
        for (const f of batch) { await request(f, 'stop'); f.element.remove(); frames.delete(f.id); }
      }
      const native = await newFrame('direct');
      await run('native-direct-auto-fallback', 1, 0, async () => [await request(native, 'direct-fallback')]);
      await request(native, 'stop'); native.element.remove(); frames.delete(native.id);
      result.pass = true;
    } catch (e) { result.failure = String(e.message).slice(0, 300); }
    finally {
      for (const f of frames.values()) { try { await request(f, 'stop'); } catch {} f.element.remove(); } frames.clear(); pending.clear();
      try { await post('/collect', result); } catch (e) { result.pass = false; result.failure = `collector: ${e.message}`; }
      results.push(result); raw.textContent = JSON.stringify({ rounds: results, summary: summarize(results), boundaries: { idle: '1000ms observation window followed by native ping/pong completion barrier', multiViewer: 'independent targeted synthetic publications, no official account roster', memory: 'performance.memory if available; implementation-specific, may aggregate iframe heaps', browserCPU: null, fallback: 'native TerminalSession + TerminalChannelTransport; synthetic terminal inventory/control; production Link Pion/direct envelope admission; server peer loss only', admission: 'production client in-memory pairing/sender pin removal + restoration, and separate fixture sender gate; not Cloud revocation propagation', timers: '12s harness failure deadlines; production timers unchanged' } }, null, 2);
      status.textContent = `Round ${round}: ${result.pass ? 'DONE / PASS' : 'FAILURE: ' + result.failure}; ${round < 3 ? 'click Start round again' : 'three rounds finished'}`; button.disabled = round >= 3;
    }
  };
}
function summarize(rounds) { const names = [...new Set(rounds.flatMap(r => r.scenarios.map(s => s.name)))]; return names.map(name => { const v = rounds.flatMap(r => r.scenarios.filter(s => s.name === name).map(s => s.duration_ms)).sort((a,b) => a-b); return { name, n: v.length, median_ms: v[Math.floor(v.length / 2)], range_ms: [v[0], v.at(-1)] }; }); }

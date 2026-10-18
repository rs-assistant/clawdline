import { test } from "node:test"
import assert from "node:assert/strict"
import { StatusCloudClient } from "./status-client.js"
import { ViewerEventLog } from "../legacy/js/net/cloud-viewer-events.js"

// An account with two Macs. On 2026-10-18 every viewer-event batch went to the
// one whose id sorts first, while the phone was making direct attempts to the
// other, so the machine those attempts were about logged none of the phone's half.
const FIRST = "mac_c58e0000"
const SECOND = "mac_c6c20000"

function twoMacs({ verified = [FIRST, SECOND], page = null, refuse = null } = {}) {
  const clock = { t: 1_700_000_000_000 }
  const client = Object.create(StatusCloudClient.prototype)
  client.viewerVerified = new Map(verified.map((machine) => [machine, { sender: "agent-" + machine }]))
  client._machineImplements = () => "yes"
  client._evidentMac = () => true
  client.classicSessionMachine = page
  client.viewerEvents = new ViewerEventLog({ now: () => clock.t })
  client.ready = true
  client.tabID = "tab-1"
  client.deviceID = "web_device"
  client.webBuild = "web-1"
  client.orchestratorSnapshots = new Map()
  client.machineDescriptors = new Map()
  const sent = []
  client._machineRequestAs = async (request, machine, word, body) => {
    sent.push({ machine, word, batch: body.batch })
    if (refuse) throw refuse
    return { batch_id: body.batch.batch_id, rows: body.batch.rows.length }
  }
  // Each send must wait for the log's spacing; move the clock past it.
  const deliver = async () => {
    const outcome = await client._deliverViewerEvents()
    clock.t += 61_000
    return outcome
  }
  return { client, sent, deliver, clock }
}

const attempt = (log, machine) => log.record("cloud.direct.attempt", { machine, kind: "carrier", stage: "open" },
  "cloud.direct.attempt|" + machine + "|" + Math.random())
const readFailed = (log, machine) => log.record("cloud.read.failed", { machine, word: "sessions.list", code: "cloud_read_timeout" },
  "cloud.read.failed|" + machine + "|" + Math.random())

function consistent(batch) {
  const c = batch.completeness
  return batch.rows.length + c.dropped_overflow + c.dropped_unflushed === c.n_to - c.n_from + 1
}

test("a direct attempt's row goes to the Mac of that attempt, not the Mac whose id sorts first", async () => {
  const { client, sent, deliver } = twoMacs()
  attempt(client.viewerEvents, SECOND)
  attempt(client.viewerEvents, SECOND)
  const outcome = await deliver()
  assert.equal(outcome.state, "delivered")
  assert.deepEqual(sent.map((s) => s.machine), [SECOND])
  assert.equal(sent[0].batch.rows.length, 2)
})

test("rows about different Macs go out as one batch per Mac, in order, each batch whole", async () => {
  const { client, sent, deliver } = twoMacs({ page: SECOND })
  const log = client.viewerEvents
  attempt(log, SECOND)
  readFailed(log, SECOND)
  readFailed(log, FIRST)
  // A page-level row names no machine: it goes to the machine the page is showing.
  log.record("cloud.frame.failed", { frame_type: "error", code: "relay_closed" })
  for (let i = 0; i < 3; i += 1) assert.equal((await deliver()).state, "delivered")
  assert.equal((await deliver()).state, "empty")
  assert.deepEqual(sent.map((s) => [s.machine, s.batch.rows.map((row) => row.event)]), [
    [SECOND, ["cloud.direct.attempt", "cloud.read.failed"]],
    [FIRST, ["cloud.read.failed"]],
    [SECOND, ["cloud.frame.failed"]],
  ])
  const ranges = sent.map((s) => [s.batch.completeness.n_from, s.batch.completeness.n_to])
  assert.deepEqual(ranges, [[1, 2], [3, 3], [4, 4]])
  assert.ok(sent.every((s) => consistent(s.batch)))
  const routes = log.snapshot().deliveries.map((entry) => [entry.state, entry.machine, entry.route, entry.rows])
  assert.deepEqual(routes, [
    ["delivered", SECOND, "row_machine", 2],
    ["delivered", FIRST, "row_machine", 1],
    ["delivered", SECOND, "page_machine", 1],
  ])
})

test("a batch cut short by another Mac's row still counts the rows the bound dropped below it", async () => {
  const { client, sent, deliver } = twoMacs()
  client.viewerEvents.limits.rows = 4
  // n1..n6 alternate; the row bound drops n3 and n4 from the middle.
  for (const machine of [FIRST, SECOND, FIRST, SECOND, FIRST, SECOND]) readFailed(client.viewerEvents, machine)
  while ((await deliver()).state === "delivered") { /* drain */ }
  assert.deepEqual(sent.map((s) => [s.machine, s.batch.completeness.n_from, s.batch.completeness.n_to,
    s.batch.completeness.dropped_overflow]), [
    [FIRST, 1, 1, 0], [SECOND, 2, 4, 2], [FIRST, 5, 5, 0], [SECOND, 6, 6, 0],
  ])
  assert.ok(sent.every((s) => consistent(s.batch)))
})

test("a batch with nowhere to go says why, once per change, in the page's own record", async () => {
  const { client, sent, deliver } = twoMacs({ verified: [] })
  attempt(client.viewerEvents, SECOND)
  assert.equal((await deliver()).why, "cloud_machine_unavailable")
  assert.equal((await deliver()).why, "cloud_machine_unavailable")
  assert.deepEqual(sent, [])
  const [entry, ...rest] = client.viewerEvents.snapshot().deliveries
  assert.deepEqual(rest, [])
  assert.equal(entry.state, "deferred")
  assert.equal(entry.why, "cloud_machine_unavailable")
  assert.equal(entry.times, 2)
})

test("a refused Mac's hold names that Mac, and a sealed batch is resent to the Mac it was sealed for", async () => {
  const refusal = Object.assign(new Error("old"), { code: "unknown_command", layer: "mac", status: 400 })
  const { client, sent, deliver } = twoMacs({ refuse: refusal })
  attempt(client.viewerEvents, SECOND)
  const first = await deliver()
  assert.equal(first.state, "blocked")
  assert.equal(first.machine, SECOND)
  // A row about the other Mac arrives meanwhile; the sealed batch still goes nowhere else.
  readFailed(client.viewerEvents, FIRST)
  const second = await deliver()
  assert.equal(second.state, "blocked")
  assert.equal(second.machine, SECOND)
  assert.equal(second.route, "sealed")
  assert.deepEqual(sent.map((s) => s.machine), [SECOND])
  assert.deepEqual(client.viewerEvents.snapshot().deliveries.map((entry) => [entry.state, entry.machine, entry.code]), [
    ["blocked", SECOND, "unknown_command"],
    ["blocked", SECOND, "unknown_command"],
  ])
})

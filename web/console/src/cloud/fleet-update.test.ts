import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs the source test with type stripping.
import { fleetUpdateFacts, readFleetUpdate } from "./fleet-update.ts"
// @ts-expect-error -- Node runs the source test with type stripping.
import { createUpdateReadStore } from "../machine/update-model.ts"

test("fleet updates ask only the named machine and preserve typed outcomes", async () => {
  const asked: string[] = []
  const client = {
    events: () => () => {},
    sessions: async () => ({ sessions: [], at: 0, scan: {} }),
    transcript: async () => ({}),
    _machineRequest: async (machine: string) => {
      asked.push(machine)
      if (machine === "old") throw Object.assign(new Error("old word"), { code: "unknown_command", status: 404 })
      if (machine === "away") throw Object.assign(new Error("timeout"), { code: "cloud_read_timeout" })
      return {
        state: "update_available", source_url: "", install_kind: "release",
        running: { version: "v0.10.0" }, latest: { version: "v0.11.0" },
        apply: { state: "rolled_back", from: "v0.10.0", to: "v0.11.0", error: { code: "health_failed" } },
      }
    },
  }
  const current = await readFleetUpdate(client, "first")
  assert.deepEqual(asked, ["first"])
  assert.deepEqual(fleetUpdateFacts({ pairing: "paired", freshness: "current" }, "v0.9.0", current), {
    running: "v0.10.0", latest: "v0.11.0", compatibility: "supported", state: "update_available",
    result: "rolled_back", from: "v0.10.0", to: "v0.11.0", errorCode: "health_failed",
  })
  assert.equal((await readFleetUpdate(client, "old")).kind, "older")
  assert.equal((await readFleetUpdate(client, "away")).kind, "unreachable")
  assert.deepEqual(asked, ["first", "old", "away"])
  assert.equal(fleetUpdateFacts({ pairing: "paired", freshness: "stale" }, "v0.10.0", current).compatibility, "offline")
  assert.equal(fleetUpdateFacts({ pairing: "not_paired", freshness: "current" }, undefined, null).compatibility, "unpaired")
  assert.equal(fleetUpdateFacts({ pairing: "paired", freshness: "current" }, undefined, null).compatibility, "unknown")
})

test("the all-machine prompt asks the selected machine for a fresh release without applying it", async () => {
  const calls: Array<[string, string, string]> = []
  const client = {
    events: () => () => {},
    sessions: async () => ({ sessions: [], at: 0, scan: {} }),
    transcript: async () => ({}),
    _machineRequest: async (machine: string, word: string, _body: object, mode: string) => {
      calls.push([machine, word, mode])
      return { state: "update_available", install_kind: "release",
        running: { version: "v1.0.1" }, latest: { version: "v1.0.2" } }
    },
  }
  const answer = await readFleetUpdate(client, "mac-a", undefined, true)
  assert.equal(answer.kind, "status")
  assert.deepEqual(calls, [["mac-a", "update-fresh", "read"]])
})

test("a failed fleet fresh read falls back to cached running status without a stale update prompt", async () => {
  const words: string[] = []
  const client = {
    events: () => () => {}, sessions: async () => ({ sessions: [], at: 0, scan: {} }),
    transcript: async () => ({}),
    _machineRequest: async (_machine: string, word: string) => {
      words.push(word)
      if (word === "update-fresh") throw Object.assign(new Error("timeout"), { status: 504 })
      return { state: "update_available", install_kind: "release",
        running: { version: "v1.0.1", stamp: "old" }, latest: { version: "v1.0.2", stamp: "new" },
        apply: { state: "healthy", to: "v1.0.1" } }
    },
  }
  const answer = await readFleetUpdate(client, "mac-a", undefined, true)
  assert.deepEqual(words, ["update-fresh", "update"])
  const facts = fleetUpdateFacts({ pairing: "paired", freshness: "current" }, undefined, answer)
  assert.equal(facts.running, "v1.0.1")
  assert.equal(facts.latest, null)
  assert.equal(facts.state, "unknown")
  assert.equal(facts.result, "healthy")
})

test("a phone fleet row asks fresh on open, foreground return, and page restoration", async () => {
  let visible = true
  const events = new Map<string, () => void>()
  const words: string[] = []
  const client = {
    events: () => () => {}, sessions: async () => ({ sessions: [], at: 0, scan: {} }),
    transcript: async () => ({}),
    _machineRequest: async (_machine: string, word: string) => {
      words.push(word)
      return { state: "current", running: { stamp: "x" }, latest: { stamp: "x" } }
    },
  }
  const on = (name: string, fn: () => void) => { events.set(name, fn); return () => { events.delete(name) } }
  const store = createUpdateReadStore((fresh) => readFleetUpdate(client, "mac-a", undefined, fresh), {
    now: () => 0, visible: () => visible,
    onVisibilityChange: (fn) => on("visibility", fn), onFocus: (fn) => on("focus", fn),
    onPageShow: (fn) => on("pageshow", fn), onBlur: (fn) => on("blur", fn),
    onOnline: (fn) => on("online", fn),
    setInterval: () => 1 as unknown as ReturnType<typeof setInterval>, clearInterval: () => {},
  })
  const stop = store.subscribe(() => {})
  await new Promise((resolve) => setImmediate(resolve))
  visible = false; events.get("visibility")!()
  visible = true; events.get("visibility")!()
  await new Promise((resolve) => setImmediate(resolve))
  events.get("pageshow")!()
  await new Promise((resolve) => setImmediate(resolve))
  assert.deepEqual(words, ["update-fresh", "update-fresh", "update-fresh"])
  stop()
})

import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node loads the TypeScript source directly.
import { ScheduleRecovery, retainScheduleRows } from "./schedules-recovery.ts"

test("the first failed list recovers when a Session answer proves the machine is reachable", () => {
  const recovery = new ScheduleRecovery()
  recovery.begin()
  recovery.missed("one")
  assert.equal(recovery.takeRetry(), false)
  recovery.machineReady("one")
  assert.equal(recovery.takeRetry(), true)
  recovery.answered("one")
  assert.equal(recovery.takeRetry(), false)
})

test("readiness during a failed in-flight read is remembered without creating a retry loop", () => {
  const recovery = new ScheduleRecovery()
  recovery.begin()
  recovery.machineReady("one")
  recovery.missed("one")
  assert.equal(recovery.takeRetry(), true)
  recovery.missed("one")
  recovery.machineReady("one")
  assert.equal(recovery.takeRetry(), false)
  recovery.begin()
  recovery.missed("one")
  assert.equal(recovery.takeRetry(), true, "a later outage must not inherit an exhausted lifetime allowance")
})

test("a second machine becoming readable gets its own recovery attempt", () => {
  const recovery = new ScheduleRecovery()
  recovery.missed("one")
  recovery.missed("two")
  recovery.machineReady("one")
  assert.equal(recovery.takeRetry(), true)
  recovery.machineReady("two")
  assert.equal(recovery.takeRetry(), true)
  assert.equal(recovery.takeRetry(), false)
  recovery.reset()
  recovery.machineReady("one")
  assert.equal(recovery.takeRetry(), false)
})

test("a partial refresh preserves the missing machine and an empty answer removes only its own rows", () => {
  const old = [{ machine: "one", id: "a" }, { machine: "two", id: "b" }]
  assert.deepEqual(retainScheduleRows(old, new Map()), old)
  assert.deepEqual(retainScheduleRows(old, new Map([["one", [{ machine: "one", id: "new" }]]])),
    [{ machine: "two", id: "b" }, { machine: "one", id: "new" }])
  assert.deepEqual(retainScheduleRows(old, new Map([["one", []]])), [{ machine: "two", id: "b" }])
  assert.deepEqual(retainScheduleRows(old, new Map([["one", []], ["two", []]])), [])
})

import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { presentVoiceSource } from "./voice-source.ts"

test("the voice source quietly names Linux while retaining the exact machine", () => {
  assert.deepEqual(presentVoiceSource({ machine: "machine-a", name: "Build host", platform: "linux" }), {
    label: "Whisper · Linux",
    description: "Whisper · Linux · Build host",
  })
})

test("the voice source recognizes the platform spellings and bounds untrusted names", () => {
  assert.equal(presentVoiceSource({ platform: "darwin", name: "Studio" }).label, "Whisper · macOS")
  assert.equal(presentVoiceSource({ platform: "windows", name: "Desk" }).label, "Whisper · Windows")
  assert.equal(presentVoiceSource({ machine: "opaque", name: "\u202e  Host\nName  " }).description,
    "Whisper · Host Name")
})

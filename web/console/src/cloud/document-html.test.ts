import { test } from "node:test"
import assert from "node:assert/strict"
import { documentAnswer, documentBytesAnswer, documentHash, documentLocatorFromHash } from "../legacy/js/net/document-links.js"

const locator = { machine: "machine-a", session: "session-a", scope: "project", path: "artifacts/report.html" }
const text = "<p>成果</p>"
const bytes = new TextEncoder().encode(text)
const body = { scope: "project", path: locator.path, media_type: "text/html; charset=utf-8", byte_count: bytes.length, data: btoa(String.fromCharCode(...bytes)) }

test("HTML bytes retain their explicit Cloud document identity and share address", () => {
  assert.equal(documentAnswer(locator, body).text, text)
  assert.equal(documentBytesAnswer(locator, body.media_type, bytes).text, text)
  assert.deepEqual(documentLocatorFromHash(documentHash(locator)), locator)
})

test("HTML still rejects mismatched identities, extra fields, byte counts and unsupported types", () => {
  for (const change of [{path: "other.html"}, {scope: "task"}, {byte_count: 1}, {data: "%%%"}, {media_type: "application/xhtml+xml"}, {extra: true}]) {
    assert.throws(() => documentAnswer(locator, {...body, ...change}))
  }
  assert.throws(() => documentBytesAnswer(locator, body.media_type, new Uint8Array([255])))
  assert.throws(() => documentHash({...locator, path: "../report.html"}))
})

test("Markdown and plain text keep their existing byte representation", () => {
  for (const [path, media_type] of [["report.md", "text/markdown; charset=utf-8"], ["report.txt", "text/plain; charset=utf-8"]]) {
    assert.equal(documentBytesAnswer({...locator, path}, media_type, bytes).text, text)
  }
})

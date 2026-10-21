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

import { validInlineImage } from "../legacy/js/view/document-html-images.js"
const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5WQAAAAASUVORK5CYII="

test("inline screenshots require canonical PNG or JPEG data and bounded dimensions", () => {
  assert.equal(validInlineImage("data:image/png;base64," + png), true)
  assert.equal(validInlineImage("data:image/jpeg;base64,/9j/2wCEAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDIBCQkJDAsMGA0NGDIhHCEyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMv/AABEIAAIAAwMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/APAGZnYszFmY5JJySaSiigG76s//2Q=="), true)
  for (const value of ["https://example.invalid/a.png", "//example.invalid/a.png", "file:///tmp/a.png", "blob:anything", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/gif;base64,R0lGODlh", "data:image/jpeg;base64," + png, "DATA:image/png;base64," + png, "data:image/png;base64, " + png, "data:image/png;base64," + png.slice(0, 10) + "\n" + png.slice(10), "data:image/png;base64,AB==", "data:image/png;base64," + png + "\n", "data:image/png;base64," + png + "#fragment"]) {
    assert.equal(validInlineImage(value), false, value)
  }
  for (const [width, height] of [[8193, 1], [4097, 4096], [0, 1], [1, 0]]) {
    const bytes = Buffer.from(png, "base64")
    bytes.writeUInt32BE(width, 16); bytes.writeUInt32BE(height, 20)
    assert.equal(validInlineImage("data:image/png;base64," + bytes.toString("base64")), false)
  }
})


test("inline screenshot PNG validation rejects animation chunks and trailing content", () => {
  const bytes = Buffer.from(png, "base64")
  const animation = Buffer.from([0,0,0,0,97,99,84,76,0,0,0,0])
  const animated = Buffer.concat([bytes.subarray(0,33),animation,bytes.subarray(33)])
  for (const invalid of [animated,Buffer.concat([bytes,Buffer.from("extra")])]) {
    assert.equal(validInlineImage("data:image/png;base64,"+invalid.toString("base64")), false)
  }
})

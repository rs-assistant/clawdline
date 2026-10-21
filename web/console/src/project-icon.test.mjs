import test from "node:test"
import assert from "node:assert/strict"
import { drawIcon, drawIconOnce } from "./legacy/js/core/project-icon.js"

globalThis.window = { devicePixelRatio: 2 }
function canvas() {
  const calls = []
  return { width: 0, height: 0, style: {}, calls,
    getContext: () => ({ clearRect() {}, fillRect(...args) { calls.push(args) }, drawImage(...args) { calls.push(args) } }) }
}
const accent = "#ABCDEF"

test("large and rectangular pixel marks stay inside their slot without cropping", () => {
  for (const [rows, cols] of [[10, 10], [64, 64], [1, 64], [64, 1]]) {
    const c = canvas()
    assert.equal(drawIcon(c, { accent, cells: Array.from({ length: rows }, () => Array(cols).fill(accent)) }, 4), true)
    assert.ok(parseFloat(c.style.width) <= 32)
    assert.ok(parseFloat(c.style.height) <= 32)
    assert.equal(parseFloat(c.style.width) / parseFloat(c.style.height), cols / rows)
    assert.equal(c.calls.length, rows * cols)
    for (const [x, y, width, height] of c.calls) {
      assert.ok(x + width <= c.width && y + height <= c.height)
    }
  }
})

test("ordinary creatures retain their dimensions and transparent cells", () => {
  const c = canvas()
  drawIcon(c, { accent, cells: [[accent, null, accent, accent, accent], [accent, accent, accent, accent, accent]] }, 4)
  assert.equal(c.style.width, "20px")
  assert.equal(c.style.height, "8px")
  assert.equal(c.calls.length, 9)
})

test("SVG marks fit the same slot and stale decodes cannot overwrite replacements", () => {
  const pending = []
  globalThis.DOMParser = class { parseFromString(source) { return {
    documentElement: { localName: "svg", setAttribute() {}, source }, querySelector: () => null,
  } } }
  globalThis.XMLSerializer = class { serializeToString(root) { return root.source } }
  globalThis.Image = class { constructor() { pending.push(this) } }
  const c = canvas()
  const icon = { accent, cells: [], svg: '<svg viewBox="0 0 20 10"><path d="M0 0h20v10z"/></svg>' }
  assert.equal(drawIconOnce(c, icon, 4), true)
  assert.equal(c.style.width, "32px")
  assert.equal(c.style.height, "32px")
  drawIconOnce(c, icon, 4)
  assert.equal(pending.length, 1)
  drawIconOnce(c, { ...icon, svg: icon.svg.replace("20 10", "30 10") }, 4)
  assert.equal(pending.length, 2)
  pending[0].onload()
  assert.equal(c.calls.length, 0)
  pending[1].onload()
  assert.equal(c.calls.length, 1)
  drawIconOnce(c, undefined, 4)
  pending[1].onload()
  assert.equal(c.calls.length, 1)
  assert.equal(c.width, 0)
})

test("unsupported pixel input is rejected before drawing", () => {
  for (const cells of [Array.from({ length: 65 }, () => [accent]), [Array(65).fill(accent)], [null]]) {
    const c = canvas()
    assert.equal(drawIcon(c, { accent, cells }, 4), false)
    assert.equal(c.calls.length, 0)
  }
})

import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips the TypeScript source in this test runner.
import { newestImageTiles } from "./image-read-order.ts"

test("newest image requests go first even if the transcript is drawn oldest first", () => {
  const tiles = [0, 1, 2, 3, 4, 5].map((slot) => ({ dataset: { artifactSlot: String(slot) } }))
  assert.deepEqual(newestImageTiles(tiles).map((tile) => tile.dataset.artifactSlot), ["5", "4", "3", "2", "1", "0"])
  assert.deepEqual(tiles.map((tile) => tile.dataset.artifactSlot), ["0", "1", "2", "3", "4", "5"])
})

test("newest image requests go first even if the transcript is drawn newest first", () => {
  const tiles = [5, 4, 3, 2, 1, 0].map((slot) => ({ dataset: { artifactSlot: String(slot) } }))
  assert.deepEqual(newestImageTiles(tiles).map((tile) => tile.dataset.artifactSlot), ["5", "4", "3", "2", "1", "0"])
})

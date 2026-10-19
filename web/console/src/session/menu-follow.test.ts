import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node loads source extensions.
import { MenuFollow } from "./menu-follow.ts"
// @ts-expect-error -- Node loads source extensions.
import { FOLLOW_MS, FOLLOW_WINDOW_MS } from "./transcript-follow.ts"

test("a lost initial menu event is recovered by a read; only the new menu finishes follow", async () => {
  let now = 0
  const follow = new MenuFollow(() => now)
  follow.start("s", "generation", "", now)
  const reads: string[] = []
  await follow.read(now, true, async (id) => { reads.push(id) }, () => {})
  follow.observe("s", "generation", "", now)
  assert.ok(follow.active, "an accepted read is not proof of a displayed menu")
  now += FOLLOW_MS
  await follow.read(now, true, async (id) => { reads.push(id) }, () => {})
  assert.deepEqual(reads, ["s", "s"])
  follow.observe("s", "generation", "model options", now)
  assert.equal(follow.active, null)
})

test("a model answer keeps following over the empty gap and stale model row until Effort arrives", () => {
  const follow = new MenuFollow()
  follow.start("s", "g", "models", 0)
  for (const menu of ["models", "", "models"]) {
    follow.observe("s", "g", menu, 1)
    assert.ok(follow.active)
  }
  follow.observe("s", "g", "effort", 2)
  assert.equal(follow.active, null)
})

test("slow reads, a failure and a hidden page do not multiply requests or replay a key", async () => {
  let now = 0
  const follow = new MenuFollow(() => now)
  follow.start("s", "g", "models", now)
  let reads = 0
  let release = () => {}
  const first = follow.read(now, true, () => { reads++; return new Promise<void>((resolve) => { release = resolve }) }, () => {})
  now += FOLLOW_MS * 3
  await follow.read(now, true, async () => { reads++ }, () => {})
  assert.equal(reads, 1)
  release()
  await first
  await follow.read(now, true, async () => { reads++ }, () => {})
  assert.equal(reads, 1, "the pace starts after the slow answer")
  now += FOLLOW_MS
  await follow.read(now, false, async () => { reads++ }, () => {})
  assert.equal(reads, 1)
  await follow.read(now, true, async () => { reads++; throw new Error("missed reply") }, () => {})
  now += FOLLOW_MS
  await follow.read(now, true, async () => { reads++ }, () => {})
  assert.equal(reads, 3)
  follow.observe("s", "g", "", FOLLOW_WINDOW_MS)
  assert.ok(follow.expired(FOLLOW_WINDOW_MS), "the shared follow window bounds automatic recovery")
  await follow.read(FOLLOW_WINDOW_MS, true, async () => { reads++ }, () => {})
  assert.equal(reads, 3)
  assert.ok(follow.active, "the manual read remains available after automatic follow expires")
})

test("switching Session or execution stops a follow and ignores a late answer", async () => {
  for (const [session, generation] of [["other", "g"], ["s", "new"]]) {
    const follow = new MenuFollow()
    follow.start("s", "g", "models", 0)
    let release = () => {}
    let changed = 0
    const pending = follow.read(0, true, () => new Promise<void>((resolve) => { release = resolve }), () => { changed++ })
    follow.observe(session, generation, "", 1)
    release()
    await pending
    assert.equal(changed, 0)
    assert.equal(follow.active, null)
  }
})

test("an ordinary unread question stops following when the Session resumes", () => {
  const follow = new MenuFollow()
  follow.start("s", "g", "", 0, false)
  follow.observe("s", "g", "", 1, false)
  assert.equal(follow.active, null)
})

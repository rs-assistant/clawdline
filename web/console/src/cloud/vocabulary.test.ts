// The vocabulary table (internal/domain/cloudvocab/vocabulary.json) against
// what this console carries: `node --test web/console/src/cloud/*.test.ts`.
//
// The Go half (internal/app/cloudops/vocabulary_test.go) drives every row
// through the machine's decode and route. This half holds the same rows to
// `carry.ts` and to `writeRoute`: one word, one list, the same console route
// spelled the same way, and the seam that parses that route naming the word.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { CARRIED, DEFERRED, NO_MACHINE_ROUTE } from "./carry.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { writeRoute } from "./relay-writer.ts"

const here = dirname(fileURLToPath(import.meta.url))
const tablePath = resolve(here, "../../../../internal/domain/cloudvocab/vocabulary.json")

interface Row {
  op: string
  console: "carried" | "deferred" | "no_machine_route"
  console_route?: string
  console_seam?: "writer" | "reader" | "client"
  console_parsed_as?: string
}

function rows(): Row[] {
  const table = JSON.parse(readFileSync(tablePath, "utf8")) as { version: number; operations: Row[] }
  assert.equal(table.version, 1)
  assert.ok(table.operations.length > 100, "the table is far shorter than the machine's vocabulary")
  return table.operations
}

/**
 * One concrete request for a `CARRIED` route as `carry.ts` spells it, or null
 * for an entry that names no route (the copied client asks on its own).
 * Optional parts in `[…]` and the query are dropped; `{a|b}` takes `a`; every
 * other placeholder is "1", which every parser here takes as an id or index.
 */
function sample(route: string): { method: string; path: string } | null {
  const space = route.indexOf(" ")
  const method = route.slice(0, space)
  let path = route.slice(space + 1)
  if (!path.startsWith("/")) return null
  path = path.split(" ")[0].split("?")[0]
  path = path.replace(/\[[^\]]*\]/g, "").replace(/[[\]]/g, "")
  path = path.replace(/\{([^}|]+)\|[^}]*\}/g, "$1").replace(/\{[^}]+\}/g, "1")
  return { method, path }
}

test("the table and carry.ts hold the same words in the same lists", () => {
  const lists: Record<Row["console"], Record<string, string>> = {
    carried: CARRIED,
    deferred: DEFERRED,
    no_machine_route: NO_MACHINE_ROUTE,
  }
  const table = rows()
  for (const [name, list] of Object.entries(lists)) {
    const inTable = table.filter((row) => row.console === name).map((row) => row.op).sort()
    assert.deepEqual(inTable, Object.keys(list).sort(), `the table's ${name} words are carry.ts's`)
  }
  assert.equal(new Set(table.map((row) => row.op)).size, table.length, "a word is in the table twice")
})

test("every carried row spells carry.ts's route and is parsed as its word", () => {
  for (const row of rows()) {
    if (row.console !== "carried") {
      assert.equal(row.console_route, undefined, `${row.op} is not carried and names a console route`)
      continue
    }
    assert.equal(row.console_route, (CARRIED as Record<string, string>)[row.op], `${row.op}: the console route`)
    const asked = sample(row.console_route!)
    if (row.console_seam === "client") {
      assert.equal(asked, null, `${row.op} says the copied client asks it, and carry.ts names a route`)
      continue
    }
    assert.ok(asked, `${row.op}: carry.ts names no route for a ${row.console_seam} word`)
    const parsed = writeRoute(asked.method, asked.path)
    if (row.console_seam === "writer") {
      assert.ok(parsed, `${row.op}: writeRoute does not parse ${asked.method} ${asked.path}`)
      assert.equal(parsed.word, row.console_parsed_as ?? row.op, `${row.op}: writeRoute's word for ${asked.method} ${asked.path}`)
    } else {
      assert.equal(row.console_seam, "reader", `${row.op}: console seam`)
      assert.equal(parsed, null, `${row.op} is the reader's and writeRoute parses ${asked.method} ${asked.path} as ${parsed?.word}`)
    }
  }
})

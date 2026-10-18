import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { directLocalNetworkShown } from "./direct-local.ts"

test("the same-network switch is shown only while direct connections are on", () => {
  assert.equal(directLocalNetworkShown({ enabled: true, terminal_direct: true, direct_local_network: false }), true)
  assert.equal(directLocalNetworkShown({ enabled: true, terminal_direct: true, direct_local_network: true }), true)
  assert.equal(directLocalNetworkShown({ enabled: true, terminal_direct: false, direct_local_network: true }), false)
  assert.equal(directLocalNetworkShown({ enabled: true, terminal_direct: false, direct_local_network: false }), false)
})

test("a line that is off shows no direct switches at all", () => {
  assert.equal(directLocalNetworkShown({ enabled: false, terminal_direct: true, direct_local_network: false }), false)
})

test("a daemon that does not report the setting has no switch to show", () => {
  assert.equal(directLocalNetworkShown({ enabled: true, terminal_direct: true }), false)
})

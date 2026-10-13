import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner resolves source .ts files.
import { notificationAddress } from "./navigation.ts"

test("a worker tap navigates only to this console's own Session address", () => {
  const here = "https://app.example.com/#all-machines"
  assert.equal(notificationAddress("/#session=%2514&machine=mac_1&generation=0123456789abcdef0123456789abcdef", here),
    "https://app.example.com/#session=%2514&machine=mac_1&generation=0123456789abcdef0123456789abcdef")
  assert.equal(notificationAddress("https://other.example.com/#session=x", here), null)
  assert.equal(notificationAddress("/other/#session=x", here), null)
  assert.equal(notificationAddress("javascript:alert(1)", here), null)
})

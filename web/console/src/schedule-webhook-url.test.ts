// The one-time webhook URL is drawn without its token:
// `node --test web/console/src/schedule-webhook-url.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { maskedWebhookURL } from "./schedule-webhook-url.ts"

const token = "not-a-real-token-only-a-test-B0oJ"
const url = "https://api.example.test/v1/schedule-webhook-trigger/" + token

test("the drawn URL keeps the host, the path and the token's last four characters", () => {
  assert.equal(maskedWebhookURL(url), "https://api.example.test/v1/schedule-webhook-trigger/••••••••B0oJ")
})

test("no more of the token than its last four characters is drawn", () => {
  const drawn = maskedWebhookURL(url)
  for (let start = 0; start + 5 <= token.length; start++) {
    assert.ok(!drawn.includes(token.slice(start, start + 5)), token.slice(start, start + 5))
  }
})

test("a short last segment is hidden whole, and a value that is not a URL is hidden whole", () => {
  assert.equal(maskedWebhookURL("https://api.example.test/hook/abcd"), "https://api.example.test/hook/••••••••")
  assert.equal(maskedWebhookURL("not a url " + token), "••••••••")
})

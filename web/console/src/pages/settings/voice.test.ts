import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in this browser-free test.
import { installVoiceModelAt, readVoiceSetupAt } from "./voice-setup-call.ts"

async function withAnswer<T>(body: unknown, run: () => Promise<T>) {
  const previous = globalThis.fetch
  let input: RequestInfo | URL | undefined
  let init: RequestInit | undefined
  globalThis.fetch = async (next, options) => {
    input = next
    init = options
    return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })
  }
  try {
    return { result: await run(), input, init }
  } finally {
    globalThis.fetch = previous
  }
}

test("voice setup reads the machine's independent binary and model state", async () => {
  const setup = {
    platform: "linux", status: "no_binary", binary: "", model: "ggml-large.bin",
    binary_dirs: ["/usr/bin", "~/.local/bin"], model_dirs: ["~/.cache/whisper"],
    binary_install: "build it", model_name: "large-v3-turbo-q5_0", model_bytes: 574041195,
  }
  const { result, input, init } = await withAnswer(setup, () => readVoiceSetupAt("/v1/voice/setup"))
  assert.deepEqual(result, setup)
  assert.equal(String(input), "/v1/voice/setup")
  assert.equal(init?.method, "GET")
})

test("the model button posts an empty bounded command and waits for the final receipt", async () => {
  const receipt = { ok: true, model: "ggml-large-v3-turbo-q5_0.bin" }
  const { result, init } = await withAnswer(receipt, () => installVoiceModelAt("/v1/voice/setup"))
  assert.deepEqual(result, receipt)
  assert.equal(init?.method, "POST")
  assert.equal(init?.body, "{}")
  assert.deepEqual(init?.headers, { "Content-Type": "application/json" })
})

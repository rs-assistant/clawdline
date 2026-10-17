import type { VoiceModelInstall, VoiceSetup } from "@clawdline/contract"

const path = "/v1/voice/setup"

export interface VoiceSetupFailures {
  transport(message: string, cause?: unknown): Error
  response(status: number, parsed: unknown, path: string): Error
}

const plainFailures: VoiceSetupFailures = {
  transport: (message, cause) => new Error(message, { cause }),
  response: (status) => new Error(`${path} answered ${status}`),
}

async function setupCall(
  url: string,
  init: RequestInit,
  failures: VoiceSetupFailures,
): Promise<VoiceSetup | VoiceModelInstall> {
  let res: Response
  try {
    res = await fetch(url, init)
  } catch (cause) {
    throw failures.transport(`${init.method ?? "GET"} ${path} did not complete`, cause)
  }
  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw failures.transport(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    throw failures.response(res.status, parsed, path)
  }
  return parsed as VoiceSetup | VoiceModelInstall
}

/** Read setup through an already resolved local or Cloud URL. */
export async function readVoiceSetupAt(url: string, failures = plainFailures): Promise<VoiceSetup> {
  return (await setupCall(url, { method: "GET", cache: "no-store" }, failures)) as VoiceSetup
}

/** Install through an already resolved local or Cloud URL. */
export async function installVoiceModelAt(url: string, failures = plainFailures): Promise<VoiceModelInstall> {
  return (await setupCall(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  }, failures)) as VoiceModelInstall
}

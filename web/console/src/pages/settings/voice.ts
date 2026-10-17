import type { VoiceLanguage, VoiceModelInstall, VoiceSetup } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../../client.js"
import { installVoiceModelAt, readVoiceSetupAt, type VoiceSetupFailures } from "./voice-setup-call.js"

const setupFailures: VoiceSetupFailures = {
  transport: (message, cause) => new TransportError(message, cause),
  response: (status, parsed, path) => isRefusal(parsed)
    ? new RefusalError(status, parsed, path)
    : new TransportError(`${path} answered ${status} with no refusal in it`),
}

/**
 * `/v1/voice/language`, what this daemon will read the next recording as and
 * who decided it (api/v1/voice.schema.json).
 *
 * The decision is the daemon's, made from its own settings file and its own
 * machine, because that is where whisper runs; this page only says it back.
 * A failure is null rather than a throw, as `readTunnelStatus` answers one: the
 * picker still works without the sentence under it.
 */
export async function readVoiceLanguage(): Promise<VoiceLanguage | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 10_000)
  try {
    const res = await fetch(client.url("/v1/voice/language"), { signal: controller.signal, cache: "no-store" })
    if (!res.ok) return null
    return (await res.json()) as VoiceLanguage
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}

/** What is installed and the exact paths and command this platform uses. */
export async function readVoiceSetup(): Promise<VoiceSetup> {
  return readVoiceSetupAt(client.url("/v1/voice/setup"), setupFailures)
}

/** Download the one pinned model. This can take minutes, so the request has no browser timer. */
export async function installVoiceModel(): Promise<VoiceModelInstall> {
  return installVoiceModelAt(client.url("/v1/voice/setup"), setupFailures)
}

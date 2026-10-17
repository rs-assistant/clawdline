export interface VoiceSource {
  machine: string
  name: string
  platform: string
}

export interface VoiceSourcePresentation {
  label: string
  description: string
}

/** The next recording's actual Whisper host, local or selected through Cloud. */
export async function voiceSource(): Promise<VoiceSourcePresentation> {
  const res = await fetch("/v1/voice/source")
  if (!res.ok) throw new Error("voice_source_unavailable")
  return presentVoiceSource(await res.json() as Partial<VoiceSource>)
}

/** Short on the row, exact in its accessible description. */
export function presentVoiceSource(source: Partial<VoiceSource>): VoiceSourcePresentation {
  const platform = platformName(source.platform)
  const name = clean(source.name)
  const machine = clean(source.machine)
  const place = platform || name || machine
  if (!place) throw new Error("voice_source_unknown")
  const exact = [platform, name || machine].filter((part, index, all) => part && all.indexOf(part) === index).join(" · ")
  return { label: `Whisper · ${place}`, description: `Whisper · ${exact}` }
}

function platformName(value: unknown): string {
  const platform = clean(value).toLowerCase()
  if (platform === "darwin" || platform === "macos") return "macOS"
  if (platform === "linux") return "Linux"
  if (platform === "windows") return "Windows"
  return ""
}

function clean(value: unknown): string {
  return typeof value === "string"
    ? Array.from(value.replace(/[\u0000-\u001f\u007f\u061c\u200b-\u200f\u202a-\u202e\u2060-\u206f\ufeff]/gu, " ")
      .replace(/\s+/gu, " ").trim()).slice(0, 80).join("")
    : ""
}

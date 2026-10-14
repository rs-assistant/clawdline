import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { test } from "node:test"

const panel = readFileSync(new URL("./PairPanel.tsx", import.meta.url), "utf8")
const gate = readFileSync(new URL("./CloudGate.tsx", import.meta.url), "utf8")
const scanner = readFileSync(new URL("./PairScanner.tsx", import.meta.url), "utf8")
const english = JSON.parse(readFileSync(new URL("../../public/catalogs/en.json", import.meta.url), "utf8"))
const taiwan = JSON.parse(readFileSync(new URL("../../public/catalogs/zh-Hant.json", import.meta.url), "utf8"))

test("pairing dialog names its current title and starts on the recommended route", () => {
  assert.match(gate, /aria-labelledby=\{pairing \? "cloud-pair-title" : undefined\}/)
  assert.match(panel, /<h2[^>]*id="cloud-pair-title"[^>]*ref=\{titleRef\}/)
  const fallbackButton = panel.slice(panel.indexOf('className="chip cloud-pair-generate"'), panel.indexOf('nextWord("cloudPairGenerate")'))
  assert.doesNotMatch(fallbackButton, /ref=\{first\}/, "the browser fallback must not steal initial focus from the scanner")
  const container = panel.indexOf('nextWord(canScan ? "cloudPairContainerPwa"')
  const recommended = panel.indexOf('nextWord("cloudPairFromMachineTitle")')
  const browser = panel.indexOf('nextWord("cloudPairFromBrowserTitle")')
  assert.ok(container > 0 && container < recommended && recommended < browser)
  assert.match(panel, /<PairScanner onScanned=\{onScan\} onUseCode=\{onBegin\} startRef=\{scanStart\}/)
  assert.match(scanner, /id="cloud-pair-scan-start"[\s\S]*ref=\{startRef\}/)
  assert.match(scanner, /id="cloud-pair-scan-use-code"[\s\S]*camera\.current\?\.stop\(\)[\s\S]*onUseCode\(\)/)
  assert.match(scanner, /type="file" accept="image\/\*" capture="environment"/)
  assert.match(scanner, /type="file" accept="image\/\*" onClick=/)
  assert.doesNotMatch(scanner, /fetch\(|sendBeacon\(|\.post\(/, "camera frames and decoded offers stay on this device")
  assert.ok(panel.indexOf("<PairScanner") < browser)
})

test("Home Screen offer exposes the complete code before optional agent instructions", () => {
  const code = panel.indexOf('id="cloud-pair-code"')
  const copy = panel.indexOf('id="cloud-pair-code-copy"')
  const advanced = panel.indexOf('id="cloud-pair-agent-advanced"')
  assert.ok(code > 0 && code < copy && copy < advanced)
  assert.match(panel, /id="cloud-pair-code"[\s\S]*value=\{state\.fragment\}/)
  assert.match(panel, /id="cloud-pair-code-copy"[\s\S]*copy\(state\.fragment, "code"\)/)
  assert.match(panel, /cloudPairCodePaste/)
  for (const key of ["cloudPairCodeLabel", "cloudPairCodeCopy", "cloudPairCodePaste", "cloudPairAgentAdvanced"]) {
    assert.ok(english[`next.${key}`], `English ${key}`)
    assert.ok(taiwan[`next.${key}`], `Taiwan Traditional Chinese ${key}`)
  }
})

test("Safari and the Home Screen app describe their separate keys and paths", () => {
  for (const catalog of [english, taiwan]) {
    assert.ok(catalog["next.cloudPairContainer"].includes("Safari"))
    assert.ok(catalog["next.cloudPairContainerPwa"].includes("Safari"))
    assert.ok(catalog["next.cloudPairFromMachineStep2Pwa"])
    assert.ok(catalog["next.cloudPairScanFallback"])
    assert.ok(catalog["next.cloudPairPhotoTake"])
    assert.ok(catalog["next.cloudPairPhotoChoose"])
  }
  assert.match(panel, /canScan \? "cloudPairContainerPwa" : "cloudPairContainer"/)
  assert.match(panel, /canScan \? "cloudPairFromMachineStep2Pwa" : "cloudPairFromMachineStep2"/)
})

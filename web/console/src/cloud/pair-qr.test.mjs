import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { test } from "node:test"
import { runInNewContext } from "node:vm"
import { encodeQr } from "../pages/settings/window/qr.ts"
import { pairingFragmentFromScan } from "../legacy/js/net/cloud-onboarding.js"
import { decodePairingInvitation } from "../legacy/js/net/cloud-pairing.js"

const decoderModule = { exports: {} }
runInNewContext(readFileSync(new URL("../../public/vendor/jsqr-1.4.0/jsQR.js", import.meta.url), "utf8"), {
  module: decoderModule,
  exports: decoderModule.exports,
})
const jsQR = decoderModule.exports

test("the shipped decoder reads pixels drawn from the desktop pairing QR encoder", () => {
  const invitation = {
    expires_at: Date.now() + 180_000,
    invitation_id: "desktop-pairing-qr-abcdefghijklmnopqrstuvwx",
    secret: Buffer.alloc(32, 7).toString("base64"),
    type: "pairing_invitation",
    v: 1,
  }
  const fragment = Buffer.from(JSON.stringify(invitation)).toString("base64url")
  const link = `https://app.clawdline.com/#pair=${fragment}`
  const code = encodeQr(link, "L")
  assert.ok(code.version >= 9 && code.version <= 12, `desktop-sized QR version ${code.version}`)
  const modulePixels = 6
  const side = (code.size + 8) * modulePixels
  const pixels = new Uint8ClampedArray(side * side * 4)
  for (let y = 0; y < side; y++) {
    for (let x = 0; x < side; x++) {
      const moduleX = Math.floor(x / modulePixels) - 4
      const moduleY = Math.floor(y / modulePixels) - 4
      const dark = moduleX >= 0 && moduleY >= 0 && moduleX < code.size && moduleY < code.size
        && code.dark[moduleY * code.size + moduleX] === 1
      const offset = (y * side + x) * 4
      pixels[offset] = pixels[offset + 1] = pixels[offset + 2] = dark ? 0 : 255
      pixels[offset + 3] = 255
    }
  }
  const decoded = jsQR(pixels, side, side, { inversionAttempts: "dontInvert" })
  assert.equal(decoded?.data, link)
  assert.equal(pairingFragmentFromScan(decoded.data), fragment)
  assert.equal(decodePairingInvitation(fragment, Date.now()).invitation_id, invitation.invitation_id)
})

test("a QR for another origin is rejected before pairing", () => {
  assert.throws(
    () => pairingFragmentFromScan("https://other.example/#pair=secret"),
    { code: "wrong_pairing_origin" },
  )
})

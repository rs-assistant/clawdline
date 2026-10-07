import type { QrDecoder } from "./pair-camera.js"

type DecoderWindow = Window & { jsQR?: QrDecoder }
let pending: Promise<QrDecoder> | null = null

/** jsQR 1.4.0 is shipped from this console's origin with its Apache-2.0 license. */
export function loadPairQrDecoder(): Promise<QrDecoder> {
  const scope = window as DecoderWindow
  if (scope.jsQR) return Promise.resolve(scope.jsQR)
  if (pending) return pending
  pending = new Promise<QrDecoder>((resolve, reject) => {
    const script = document.createElement("script")
    script.src = "/vendor/jsqr-1.4.0/jsQR.js"
    script.async = true
    script.onload = () => {
      if (scope.jsQR) resolve(scope.jsQR)
      else reject(Object.assign(new Error("decoder_unavailable"), { code: "decoder_unavailable" }))
    }
    script.onerror = () => reject(Object.assign(new Error("decoder_unavailable"), { code: "decoder_unavailable" }))
    document.head.appendChild(script)
  }).catch((error) => {
    pending = null
    throw error
  })
  return pending
}

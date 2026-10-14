import { useLayoutEffect, useRef, useState, type ChangeEvent, type RefObject } from "react"
import { nextWord } from "../next-strings.js"
import { decodePairingInvitation, pairingFragmentFromScan } from "./copied.js"
import { PairCamera, invitationError, type CameraIssue, type CameraState } from "./pair-camera.js"
import { loadPairQrDecoder } from "./pair-qr-decoder.js"

const issueKey: Record<CameraIssue, Parameters<typeof nextWord>[0]> = {
  unsupported: "cloudPairScanUnsupported",
  permission_denied: "cloudPairScanDenied",
  camera_missing: "cloudPairScanMissing",
  camera_busy: "cloudPairScanBusy",
  decoder_unavailable: "cloudPairScanDecoderFailed",
  camera_failed: "cloudPairScanFailed",
  background: "cloudPairScanBackground",
  not_pairing_qr: "cloudPairScanWrongCode",
  wrong_pairing_origin: "cloudPairScanWrongOrigin",
  invitation_expired: "cloudPairScanExpired",
  bad_invitation: "cloudPairScanWrongCode",
  photo_unreadable: "cloudPairPhotoUnreadable",
  photo_no_qr: "cloudPairPhotoNoQr",
}

function checkedFragment(value: string): string {
  const fragment = pairingFragmentFromScan(value)
  decodePairingInvitation(fragment, Date.now())
  return fragment
}

/** Scan inside the PWA so the invitation stays with this app's own key store. */
export function PairScanner({ onScanned, onUseCode, startRef }: {
  onScanned: (fragment: string) => void
  onUseCode: () => void
  startRef: RefObject<HTMLButtonElement | null>
}) {
  const video = useRef<HTMLVideoElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const camera = useRef<PairCamera | null>(null)
  const alive = useRef(true)
  const onScannedRef = useRef(onScanned)
  onScannedRef.current = onScanned
  const [state, setState] = useState<CameraState>("idle")
  const [issue, setIssue] = useState<CameraIssue | null>(null)

  useLayoutEffect(() => {
    if (state === "opening") cancelRef.current?.focus({ preventScroll: true })
    if (state === "idle" && issue) startRef.current?.focus({ preventScroll: true })
  }, [state, issue, startRef])

  useLayoutEffect(() => {
    alive.current = true
    if (!video.current || !canvas.current) return
    camera.current = new PairCamera({
      video: video.current,
      canvas: canvas.current,
      media: navigator.mediaDevices ?? null,
      visibility: document,
      loadDecoder: loadPairQrDecoder,
      check: checkedFragment,
      found: (fragment) => onScannedRef.current(fragment),
      state: setState,
      issue: setIssue,
      requestFrame: (callback) => requestAnimationFrame(callback),
      cancelFrame: (handle) => cancelAnimationFrame(handle),
      now: () => performance.now(),
    })
    return () => {
      alive.current = false
      camera.current?.dispose()
      camera.current = null
    }
  }, [])

  const readPhoto = async (file: File) => {
    if (!file.type.startsWith("image/")) {
      setIssue("photo_unreadable")
      return
    }
    let url = ""
    try {
      url = URL.createObjectURL(file)
      const image = document.createElement("img")
      await new Promise<void>((resolve, reject) => {
        image.onload = () => resolve()
        image.onerror = () => reject(new Error("photo_unreadable"))
        image.src = url
      })
      if (!alive.current) return
      const canvasElement = canvas.current
      const context = canvasElement?.getContext("2d", { willReadFrequently: true })
      if (!canvasElement || !context || !image.naturalWidth || !image.naturalHeight) throw new Error("photo_unreadable")
      const scale = Math.min(1, 1600 / Math.max(image.naturalWidth, image.naturalHeight))
      canvasElement.width = Math.round(image.naturalWidth * scale)
      canvasElement.height = Math.round(image.naturalHeight * scale)
      context.drawImage(image, 0, 0, canvasElement.width, canvasElement.height)
      const pixels = context.getImageData(0, 0, canvasElement.width, canvasElement.height)
      const decoder = await loadPairQrDecoder()
      if (!alive.current) return
      const value = decoder(pixels.data, pixels.width, pixels.height)?.data
      if (!value) {
        setIssue("photo_no_qr")
        return
      }
      try {
        onScannedRef.current(checkedFragment(value))
      } catch (error) {
        setIssue(invitationError(error))
      }
    } catch (error) {
      if (!alive.current) return
      if (error && typeof error === "object" && "code" in error && error.code === "decoder_unavailable") setIssue("decoder_unavailable")
      else setIssue("photo_unreadable")
    } finally {
      if (url) URL.revokeObjectURL(url)
    }
  }

  const choosePhoto = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0]
    event.currentTarget.value = ""
    if (!file) return
    setIssue(null)
    void readPhoto(file)
  }

  const cancel = () => {
    camera.current?.stop()
    setIssue(null)
    startRef.current?.focus({ preventScroll: true })
  }

  return (
    <div className="cloud-pair-scanner">
      <button
        className="go cloud-pair-scan-start"
        type="button"
        id="cloud-pair-scan-start"
        ref={startRef}
        hidden={state !== "idle"}
        onClick={() => { setIssue(null); void camera.current?.start() }}
      >
        {nextWord("cloudPairScanStart")}
      </button>
      <video ref={video} className="cloud-pair-scan-video" playsInline muted autoPlay hidden={state === "idle"} aria-label={nextWord("cloudPairScanVideo")} />
      <canvas ref={canvas} hidden aria-hidden="true" />
      {state !== "idle" && (
        <div className="cloud-pair-scan-controls">
          <span role="status">{nextWord(state === "opening" ? "cloudPairScanOpening" : "cloudPairScanReady")}</span>
          <button className="chip" type="button" id="cloud-pair-scan-cancel" ref={cancelRef} onClick={cancel}>{nextWord("cloudPairScanCancel")}</button>
        </div>
      )}
      <p className="fine">{nextWord("cloudPairPhotoHint")}</p>
      <div className="cloud-pair-photo-options">
        <label className="chip cloud-pair-photo-option">
          {nextWord("cloudPairPhotoTake")}
          <input type="file" accept="image/*" capture="environment" onClick={() => camera.current?.stop()} onChange={choosePhoto} />
        </label>
        <label className="chip cloud-pair-photo-option">
          {nextWord("cloudPairPhotoChoose")}
          <input type="file" accept="image/*" onClick={() => camera.current?.stop()} onChange={choosePhoto} />
        </label>
      </div>
      {issue && (
        <div className="cloud-pair-scan-issue">
          <p role="status">{nextWord(issueKey[issue])} {nextWord("cloudPairScanFallback")}</p>
          <button className="chip" type="button" id="cloud-pair-scan-use-code" onClick={() => {
            camera.current?.stop()
            onUseCode()
          }}>{nextWord("cloudPairGenerate")}</button>
        </div>
      )}
    </div>
  )
}

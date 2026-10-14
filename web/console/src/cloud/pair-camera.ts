/** A camera belongs to one visible pairing panel. No frame leaves the page. */
export type QrDecoder = (pixels: Uint8ClampedArray, width: number, height: number) => { data: string } | null
export type CameraState = "idle" | "opening" | "scanning"
export type CameraIssue =
  | "unsupported" | "permission_denied" | "camera_missing" | "camera_busy"
  | "decoder_unavailable" | "camera_failed" | "background" | "not_pairing_qr"
  | "wrong_pairing_origin" | "invitation_expired" | "bad_invitation"
  | "photo_unreadable" | "photo_no_qr"

type CameraOptions = {
  video: HTMLVideoElement
  canvas: HTMLCanvasElement
  media: Pick<MediaDevices, "getUserMedia"> | null
  visibility: Pick<Document, "addEventListener" | "removeEventListener" | "visibilityState">
  loadDecoder: () => Promise<QrDecoder>
  check: (value: string) => string
  found: (fragment: string) => void
  state: (state: CameraState) => void
  issue: (issue: CameraIssue) => void
  requestFrame: (callback: FrameRequestCallback) => number
  cancelFrame: (handle: number) => void
  now: () => number
}

const SCAN_WIDTH = 640
const SCAN_INTERVAL_MS = 200

export class PairCamera {
  private readonly options: CameraOptions
  private generation = 0
  private stream: MediaStream | null = null
  private frame: number | null = null
  private active = false
  private disposed = false
  private lastScan = -SCAN_INTERVAL_MS
  private decoder: QrDecoder | null = null
  private readonly visibilityChanged = () => {
    if (this.options.visibility.visibilityState !== "hidden" || !this.active) return
    this.stop()
    this.options.issue("background")
  }

  constructor(options: CameraOptions) {
    this.options = options
    options.visibility.addEventListener("visibilitychange", this.visibilityChanged)
  }

  async start(): Promise<void> {
    if (this.active || this.disposed) return
    if (!this.options.media?.getUserMedia) {
      this.options.issue("unsupported")
      return
    }
    this.active = true
    const generation = ++this.generation
    this.options.state("opening")
    try {
      // Request camera access in the click handler, before the first await on
      // the decoder, so iOS still treats this as a person-initiated request.
      const stream = await this.options.media.getUserMedia({ audio: false, video: { facingMode: { ideal: "environment" } } })
      if (!this.current(generation)) {
        stream.getTracks().forEach((track) => track.stop())
        return
      }
      this.stream = stream
      this.decoder = await this.options.loadDecoder()
      if (!this.current(generation)) return
      this.options.video.srcObject = stream
      await this.options.video.play()
      if (!this.current(generation)) return
      this.options.state("scanning")
      this.frame = this.options.requestFrame(() => this.scan(generation))
    } catch (error) {
      if (!this.current(generation)) return
      const issue = cameraError(error)
      this.stop()
      this.options.issue(issue)
    }
  }

  stop(): void {
    ++this.generation
    this.active = false
    if (this.frame !== null) this.options.cancelFrame(this.frame)
    this.frame = null
    this.stream?.getTracks().forEach((track) => track.stop())
    this.stream = null
    this.options.video.pause()
    this.options.video.srcObject = null
    if (!this.disposed) this.options.state("idle")
  }

  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    this.options.visibility.removeEventListener("visibilitychange", this.visibilityChanged)
    this.stop()
  }

  private current(generation: number): boolean {
    return this.active && !this.disposed && generation === this.generation
  }

  private scan(generation: number): void {
    this.frame = null
    if (!this.current(generation)) return
    const video = this.options.video
    const now = this.options.now()
    if (video.readyState >= 2 && video.videoWidth > 0 && now - this.lastScan >= SCAN_INTERVAL_MS) {
      this.lastScan = now
      const width = Math.min(video.videoWidth, SCAN_WIDTH)
      const height = Math.round(video.videoHeight * width / video.videoWidth)
      const canvas = this.options.canvas
      canvas.width = width
      canvas.height = height
      const context = canvas.getContext("2d", { willReadFrequently: true })
      if (!context) {
        this.stop()
        this.options.issue("camera_failed")
        return
      }
      try {
        context.drawImage(video, 0, 0, width, height)
        const pixels = context.getImageData(0, 0, width, height).data
        const value = this.decoder?.(pixels, width, height)?.data
        if (value) {
          try {
            const fragment = this.options.check(value)
            this.stop()
            this.options.found(fragment)
            return
          } catch (error) {
            this.options.issue(invitationError(error))
          }
        }
      } catch {
        this.stop()
        this.options.issue("camera_failed")
        return
      }
    }
    if (this.current(generation)) this.frame = this.options.requestFrame(() => this.scan(generation))
  }
}

export function invitationError(error: unknown): CameraIssue {
  const code = error && typeof error === "object" && "code" in error ? error.code : null
  if (code === "wrong_pairing_origin" || code === "not_pairing_qr" || code === "invitation_expired") return code
  return "bad_invitation"
}

function cameraError(error: unknown): CameraIssue {
  const named = error && typeof error === "object" && "name" in error ? error.name : null
  if (named === "NotAllowedError" || named === "PermissionDeniedError") return "permission_denied"
  if (named === "NotFoundError" || named === "DevicesNotFoundError") return "camera_missing"
  if (named === "NotReadableError" || named === "TrackStartError") return "camera_busy"
  const code = error && typeof error === "object" && "code" in error ? error.code : null
  if (code === "decoder_unavailable") return code
  return "camera_failed"
}

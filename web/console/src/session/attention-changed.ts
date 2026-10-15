/** A verified detail read can be newer than either Session list. */
const EVENT = "clawdline:session-attention-read"

export function announceAttentionRead(sessionID: string, count: number): void {
  window.dispatchEvent(new CustomEvent(EVENT, { detail: { sessionID, count } }))
}

export function onAttentionRead(receive: (sessionID: string, count: number) => void): () => void {
  const handler = (event: Event) => {
    const detail = (event as CustomEvent<{ sessionID: string; count: number }>).detail
    if (detail && typeof detail.sessionID === "string" && Number.isSafeInteger(detail.count) && detail.count >= 0)
      receive(detail.sessionID, detail.count)
  }
  window.addEventListener(EVENT, handler)
  return () => window.removeEventListener(EVENT, handler)
}

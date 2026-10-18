/**
 * The hosted fetch adapter may wait for a relay receipt without observing AbortSignal.
 *
 * A read that does observe it may answer when it fires: the relay reader ends
 * its wait for a machine's next pass with the list it already holds
 * (`initialSnapshot`, cloud/relay-reader.ts). That answer arrives a tick after
 * the abort, so a deadline that rejected at once threw it away and said the
 * daemon had not answered. `answerMs` is how long such an answer is waited for
 * after the deadline; zero rejects at the deadline as before.
 */
export async function fetchWithDeadline(path: string, init: RequestInit, timeoutMs: number, answerMs = 0): Promise<Response> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  let late: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      fetch(path, { ...init, signal: controller.signal }),
      new Promise<never>((_, reject) => controller.signal.addEventListener("abort", () => {
        const expire = () => reject(new DOMException("Timed out", "AbortError"))
        if (answerMs > 0) late = setTimeout(expire, answerMs)
        else expire()
      }, { once: true })),
    ])
  } finally {
    clearTimeout(timer)
    clearTimeout(late)
  }
}

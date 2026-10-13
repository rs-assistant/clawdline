// @ts-expect-error -- Node's strip-types runner loads this source in its suite.
import { keepConnected, type CloudConnection, type CloudSession, type CloudUpdate } from "./copied.ts"

// The copied connection loop uses these same two limits. Reuse them for its
// recovery wrapper so a parked line makes one probe, never a fresh burst.
const PROBE_MS = 30_000
const MIN_ATTEMPT_SPACING_MS = 2_000

type Connect = typeof keepConnected
type Timer = ReturnType<typeof setTimeout>

export interface RecoveringCloudConnection extends CloudConnection {
  /** A retained message card asks for the next attempt before it reads back. */
  retry(): void
}

/** Keep the retired-page copy pinned, and own the hosted console's recovery here. */
export function keepConnectedRecovering(
  session: CloudSession,
  options: { onState: (update: CloudUpdate) => void },
  dependencies: {
    connect?: Connect
    now?: () => number
    setTimeout?: typeof setTimeout
    clearTimeout?: typeof clearTimeout
  } = {},
): RecoveringCloudConnection {
  const connect = dependencies.connect ?? keepConnected
  const now = dependencies.now ?? Date.now
  const setTimer = dependencies.setTimeout ?? globalThis.setTimeout
  const cancelTimer = dependencies.clearTimeout ?? globalThis.clearTimeout
  let line: CloudConnection | null = null
  let timer: Timer | null = null
  let stopped = false
  let connected = false
  let restarting = false
  let lastExhaustedAt = 0
  let epoch = 0
  let finish!: () => void
  const done = new Promise<void>((resolve) => { finish = resolve })

  function clearProbe(): void {
    if (timer !== null) cancelTimer(timer)
    timer = null
  }

  function schedule(delay: number): void {
    clearProbe()
    timer = setTimer(() => {
      timer = null
      restart()
    }, delay)
  }

  function start(probe: boolean): void {
    const generation = ++epoch
    const next = connect(session, {
      onState(update) {
        if (stopped || generation !== epoch) return
        if (update.state === "connected") {
          connected = true
          clearProbe()
        } else if (update.state === "reconnecting" || update.state === "retrying" ||
          update.state === "terminal_error") {
          connected = false
        }
        if (update.state === "terminal_error" && update.reason === "retries_exhausted" &&
          update.error?.code !== "forbidden") {
          lastExhaustedAt = now()
          options.onState({ state: "retrying", error: update.error, afterMs: PROBE_MS })
          schedule(PROBE_MS)
          return
        }
        options.onState(update)
      },
      ...(probe ? { maximumConnectFailures: 1, initialBackoffMs: PROBE_MS,
        maximumBackoffMs: PROBE_MS } : {}),
    })
    line = next
    void next.done.then(() => {
      if (line === next && timer === null) finish()
    }, () => {
      if (line === next && timer === null) finish()
    })
  }

  function restart(): void {
    if (stopped || restarting) return
    restarting = true
    const generation = ++epoch
    const previous = line
    line = null
    previous?.stop()
    void (previous?.done ?? Promise.resolve()).then(() => {
      if (stopped || generation !== epoch) return
      restarting = false
      start(true)
    }, () => {
      if (stopped || generation !== epoch) return
      restarting = false
      start(true)
    })
  }

  start(false)
  return {
    done,
    stop() {
      if (stopped) return
      stopped = true
      clearProbe()
      line?.stop()
      void (line?.done ?? Promise.resolve()).then(finish, finish)
    },
    retry() {
      if (stopped || connected || restarting) return
      if (timer !== null) {
        schedule(Math.max(0, MIN_ATTEMPT_SPACING_MS - (now() - lastExhaustedAt)))
        return
      }
      // During ordinary backoff the copied loop already knows the previous
      // attempt time. A visible wake advances it without breaking that bound.
      const client = (session as CloudSession & { client?: { ready?: boolean;
        lifecycle?: (reason: string) => boolean } }).client
      if (client?.ready === false) client.lifecycle?.("visible")
    },
  }
}

import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { focusModalContainer } from "../modal-focus.js"
import {
  DeviceLimitRun,
  type DeviceLimitFailure,
  type DeviceLimitState,
  type RecoveryDevice,
  type RecoverySession,
} from "./device-limit.js"

/**
 * The card for an account that has no viewer-device slot left
 * (`device-limit.ts`): what the limit is, the devices using it with the one
 * unused longest first and marked, and a way to remove one so this browser can
 * carry on signing in. Each row is a name and how long ago it was used; the
 * dates a device was added and last seen are left out because neither picks
 * which one to remove faster than the order does. Every state it can be in
 * has a button on it, because a card with none is a dead end for a browser
 * that has no other page to go to (docs/records/first-run-audit.md B12).
 */
export function DeviceLimitPanel(props: {
  session: (RecoverySession & { signInURL(): string }) | null
  tier: string
  limit: number | null
  /** Ask for the session again; the gate's own start. */
  onContinue: () => void
  signInLabel: string
}) {
  const { session, tier, limit, onContinue, signInLabel } = props
  const [state, setState] = useState<DeviceLimitState>({ phase: "loading" })
  const run = useRef<DeviceLimitRun | null>(null)
  const askDialog = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!session) return
    const next = new DeviceLimitRun(session, { onState: setState, continueSignIn: onContinue, tier, limit })
    run.current = next
    void next.load()
    return () => {
      next.stop()
      if (run.current === next) run.current = null
    }
  }, [session, tier, limit, onContinue])
  const asking = state.phase === "listed" ? state.asking : null
  useLayoutEffect(() => {
    if (asking) focusModalContainer(askDialog.current)
  }, [asking])

  const shownLimit = state.phase === "listed" ? state.limit : limit
  const title =
    shownLimit === null ? nextWord("cloudDeviceLimitTitleNoCount") : nextWord("cloudDeviceLimitTitle", { limit: shownLimit })
  // The plan's name is not shown: free and Pro both allow five viewer devices
  // (Cloud's `domain/entitlements.ts`), so "the Pro plan allows 5" reads as
  // if the five came with Pro.
  const lede = shownLimit === null ? nextWord("cloudDeviceLimitNoCount") : nextWord("cloudDeviceLimitNoTier", { limit: shownLimit })
  const signIn = (
    <button className="go" type="button" id="cloud-device-sign-in" onClick={() => session && location.assign(session.signInURL())}>
      {signInLabel}
    </button>
  )

  function rows(listed: Extract<DeviceLimitState, { phase: "listed" }>) {
    const now = Date.now()
    return (
      <>
        <p className="cloud-device-order">{nextWord("cloudDeviceOrder")}</p>
        <ul className="cloud-devices" id="cloud-devices">
          {listed.devices.map((device, index) => (
            <li key={device.id} data-device={device.id} className={index === 0 ? "oldest" : undefined}>
              <DeviceIcon kind={device.kind} />
              <span className="cloud-device-text">
                <span className="cloud-device-name">{device.name}</span>
                <span className="cloud-device-facts">{idle(device, now, index === 0)}</span>
              </span>
              {listed.revoking === device.id ? (
                <span className="say calm">{nextWord("cloudDeviceRemoving", { name: device.name })}</span>
              ) : listed.asking !== device.id ? (
                <button
                  type="button"
                  className="chip cloud-device-remove"
                  disabled={listed.revoking !== null}
                  onClick={() => void run.current?.remove(device.id)}
                >
                  {nextWord("cloudDeviceRemove")}
                </button>
              ) : null}
              {listed.asking === device.id && (
                <div className="cloud-device-ask" role="alertdialog" ref={askDialog} aria-describedby={"cloud-device-ask-" + device.id}>
                  <p className="say" id={"cloud-device-ask-" + device.id}>
                    {nextWord("cloudDeviceRecentAsk", { time: ago(device.last_seen_at, now) })}
                  </p>
                  <div className="buttons">
                    <button type="button" className="chip" onClick={() => run.current?.ask(null)}>
                      {nextWord("cloudDeviceRemoveCancel")}
                    </button>
                    <button type="button" className="chip confirm-go" onClick={() => void run.current?.revoke(device.id)}>
                      {nextWord("cloudDeviceRemoveConfirm")}
                    </button>
                  </div>
                </div>
              )}
            </li>
          ))}
        </ul>
      </>
    )
  }

  function content() {
    switch (state.phase) {
      case "loading":
        return (
          <>
            <p className="say calm" aria-busy="true">{nextWord("cloudDeviceLimitLoading")}</p>
            {/* Waiting has a way out too: the list may never come. */}
            <button className="go" type="button" onClick={onContinue}>
              {nextWord("cloudDeviceLimitContinue")}
            </button>
          </>
        )
      case "list_failed":
        return state.failure.expired ? (
          <>
            <Failure word="cloudDeviceTicketExpiredShort" failure={state.failure} />
            {signIn}
          </>
        ) : (
          <>
            <Failure word="cloudDeviceListFailedShort" failure={state.failure} />
            <button className="go" type="button" onClick={() => void run.current?.load()}>
              {nextWord("cloudRetry")}
            </button>
          </>
        )
      case "continuing":
        return <p className="say calm">{nextWord("cloudDeviceRemoved", { name: state.removed.name })}</p>
      case "listed":
        return (
          <>
            {state.failure &&
              (state.failure.expired ? (
                <>
                  <Failure word="cloudDeviceTicketExpiredShort" failure={state.failure} />
                  {signIn}
                </>
              ) : (
                <Failure word="cloudDeviceRemoveFailedShort" failure={state.failure} />
              ))}
            {state.devices.length === 0 ? (
              <>
                <p className="say">{nextWord("cloudDeviceLimitEmpty")}</p>
                <button className="go" type="button" onClick={onContinue}>
                  {nextWord("cloudDeviceLimitContinue")}
                </button>
              </>
            ) : (
              <>
                {rows(state)}
                {state.failure && !state.failure.expired && (
                  <button className="go" type="button" onClick={() => void run.current?.load()}>
                    {nextWord("cloudRetry")}
                  </button>
                )}
              </>
            )}
          </>
        )
    }
  }

  return (
    <div className="cloud-device-limit">
      <p className="cloud-device-title">{title}</p>
      <p className="fine">{lede}</p>
      {session ? content() : signIn}
    </div>
  )
}

/** One failure in a sentence, with the control plane's code under it, smaller. */
function Failure(props: {
  word: "cloudDeviceTicketExpiredShort" | "cloudDeviceListFailedShort" | "cloudDeviceRemoveFailedShort"
  failure: DeviceLimitFailure
}) {
  return (
    <div className="cloud-device-failure">
      <p className="say">{nextWord(props.word)}</p>
      <p className="cloud-device-code">{nextWord("cloudDeviceCode", { code: props.failure.code })}</p>
    </div>
  )
}

function kindWord(kind: string): string {
  switch (kind) {
    case "ios":
      return nextWord("cloudDeviceKindIOS")
    case "android":
      return nextWord("cloudDeviceKindAndroid")
    default:
      return nextWord("cloudDeviceKindBrowser")
  }
}

/** A phone or a browser window, named for a screen reader by the device's kind. */
function DeviceIcon(props: { kind: string }) {
  const phone = props.kind === "ios" || props.kind === "android"
  return (
    <svg className="cloud-device-icon" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" role="img" aria-label={kindWord(props.kind)}>
      {phone ? (
        <>
          <rect x="6" y="2.5" width="12" height="19" rx="2.5" />
          <path d="M10.5 18.5h3" />
        </>
      ) : (
        <>
          <rect x="2.5" y="4" width="19" height="16" rx="2.5" />
          <path d="M2.5 8.5h19" />
        </>
      )}
    </svg>
  )
}

/**
 * How long ago a time the control plane wrote was, in the page's language:
 * minutes, hours, days, then months. Always a number ("1 day ago", not
 * "yesterday"), so it reads the same in every sentence it is put in. The raw
 * text if it will not parse.
 */
function ago(at: string | null, now: number): string {
  const parsed = at ? Date.parse(at) : NaN
  if (!Number.isFinite(parsed)) return at ?? ""
  const minutes = Math.max(1, Math.round((now - parsed) / 60_000))
  const [value, unit]: [number, Intl.RelativeTimeFormatUnit] =
    minutes < 90
      ? [minutes, "minute"]
      : minutes < 36 * 60
        ? [Math.round(minutes / 60), "hour"]
        : minutes < 30 * 24 * 60
          ? [Math.round(minutes / (24 * 60)), "day"]
          : [Math.round(minutes / (30 * 24 * 60)), "month"]
  try {
    return new Intl.RelativeTimeFormat(document.documentElement.lang || undefined, { numeric: "always" }).format(-value, unit)
  } catch {
    // refusal-ok: a browser refusing a locale is not a control-plane refusal.
    return new Date(parsed).toLocaleString()
  }
}

/** A row's second line: how long ago it was used, and for the first row that it is the one unused longest. */
function idle(device: RecoveryDevice, now: number, oldest: boolean): string {
  if (!device.last_seen_at) return nextWord("cloudDeviceSeenNever")
  const time = ago(device.last_seen_at, now)
  return oldest ? nextWord("cloudDeviceOldest", { time }) : time
}

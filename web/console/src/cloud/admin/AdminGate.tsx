import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { chooseTransport, cloudViewerDeviceMetadata, newCloudSession, readCloudConfig, type CloudConfig, type EnsuredSession } from "../copied.js"
import { DeviceLimitPanel } from "../DeviceLimitPanel.js"
import { bootCatalog } from "../../catalog.js"
import { catalogURL } from "../strings.js"
import { AdminDashboard } from "./AdminDashboard.js"
import { loadOverview, type Overview } from "./api.js"
import { adminLocale, adminWords, type Say } from "./words.js"
import "./admin.css"

/*
 * The hosted console's `/admin`: Cloud's operations overview, for the accounts
 * the API names superusers. It stands apart from `CloudGate` on purpose — an
 * operator with no machine paired, or a relay that is down, still reaches it —
 * so it signs in through the same revocable cookie session and nothing else:
 * no relay, no pairing. Who may see the numbers is the API's decision, made on
 * the GitHub identity behind the session; this page only draws its answer.
 */

type SessionHandle = ReturnType<typeof newCloudSession>

type Phase =
  | { kind: "misdeclared" }
  | { kind: "checking" }
  | { kind: "sign_in"; url: string }
  | { kind: "device_limit"; tier: string; limit: number | null }
  | { kind: "session_failed"; code: string }
  | { kind: "loading" }
  | { kind: "forbidden" }
  | { kind: "failed"; code: string }
  | { kind: "ready"; overview: Overview; refreshing: boolean; stale: { code: string } | null }

function configFrom(declared: string): CloudConfig | null {
  try {
    const config = readCloudConfig({ __clawdlineCloud: JSON.parse(declared) })
    if (!config) return null
    return chooseTransport({ mock: false, origin: location.origin, config }) === "cloud" ? config : null
  } catch {
    return null
  }
}

function failureCode(error: unknown): string {
  const code = error && typeof error === "object" && "code" in error ? (error as { code: unknown }).code : null
  return typeof code === "string" && code ? code : "unavailable"
}

export function AdminGate({ declared }: { declared: string }) {
  const config = useMemo(() => configFrom(declared), [declared])
  const locale = useMemo(() => adminLocale(navigator.languages?.length ? navigator.languages : [navigator.language]), [])
  const say: Say = useMemo(() => adminWords(locale), [locale])
  const [phase, setPhase] = useState<Phase>(config ? { kind: "checking" } : { kind: "misdeclared" })
  const [catalogReady, setCatalogReady] = useState(false)
  const session = useRef<SessionHandle | null>(null)
  const generation = useRef(0)

  useEffect(() => {
    document.documentElement.lang = locale
    document.documentElement.dir = "ltr"
    document.title = say("docTitle")
    document.documentElement.classList.remove("booting")
  }, [locale, say])

  useEffect(() => {
    if (!config) return
    let active = true
    void bootCatalog((tag) => catalogURL(config, tag, document.baseURI)).finally(() => {
      if (!active) return
      document.documentElement.lang = locale
      setCatalogReady(true)
    })
    return () => { active = false }
  }, [config, locale])

  const fetchOverview = useCallback(async (keep: Overview | null) => {
    if (!config) return
    const mine = ++generation.current
    if (!keep) setPhase({ kind: "loading" })
    const result = await loadOverview((url, init) => fetch(url, init), config.apiOrigin)
    if (mine !== generation.current) return
    switch (result.kind) {
      case "ready":
        setPhase({ kind: "ready", overview: result.overview, refreshing: false, stale: null })
        return
      case "signed_out":
        setPhase({ kind: "sign_in", url: session.current?.signInURL() ?? config.apiOrigin })
        return
      case "forbidden":
        setPhase({ kind: "forbidden" })
        return
      case "failed":
        // A refresh that fails keeps the figures already confirmed, dated, and
        // says so; a first load that fails shows none rather than zeros.
        setPhase(keep
          ? { kind: "ready", overview: keep, refreshing: false, stale: { code: result.code } }
          : { kind: "failed", code: result.code })
    }
  }, [config])

  const start = useCallback(async () => {
    if (!config) return
    const mine = ++generation.current
    setPhase({ kind: "checking" })
    const device = cloudViewerDeviceMetadata(window)
    const next = newCloudSession({
      config,
      deviceKind: device.kind,
      deviceName: device.name,
      returnTo: config.appOrigin + "/admin",
    })
    session.current = next
    let ensured: EnsuredSession
    try {
      ensured = await next.ensureSession()
    } catch (error) {
      if (mine !== generation.current) return
      const code = failureCode(error)
      // A device the control plane revoked, or no longer lists, is a session
      // that has to be made again: signing in is the way, not a retry.
      setPhase(code === "revoked" ? { kind: "sign_in", url: next.signInURL() } : { kind: "session_failed", code })
      return
    }
    if (mine !== generation.current) return
    if (ensured.state === "sign_in") setPhase({ kind: "sign_in", url: ensured.url })
    else if (ensured.state === "device_limit_reached") setPhase({ kind: "device_limit", tier: ensured.tier, limit: ensured.limit })
    else await fetchOverview(null)
  }, [config, fetchOverview])

  useEffect(() => {
    void start()
    return () => {
      generation.current++
    }
  }, [start])

  const refresh = useCallback(() => {
    if (phase.kind !== "ready") {
      void start()
      return
    }
    setPhase({ ...phase, refreshing: true })
    void fetchOverview(phase.overview)
  }, [phase, start, fetchOverview])

  const consoleHref = config ? config.appOrigin + "/" : "/"

  return (
    <div className="adm">
      <header className="adm-bar">
        <div className="adm-bar-in">
          <a className="adm-back" href={consoleHref}>
            <span aria-hidden="true">←</span> {say("back")}
          </a>
          <div className="adm-heading">
            <h1>{say("title")}</h1>
            <p className="adm-sub">{say("subtitle")}</p>
          </div>
          {phase.kind === "ready" ? (
            <div className="adm-actions">
              <p className="adm-stamp">
                {say("generated", { time: stamp(phase.overview.generated_at, locale) })}
              </p>
              <button type="button" className="adm-btn" onClick={refresh} disabled={phase.refreshing} aria-busy={phase.refreshing}>
                {phase.refreshing ? say("refreshing") : say("refresh")}
              </button>
            </div>
          ) : null}
        </div>
      </header>
      <main className="adm-main" aria-live="polite" aria-busy={phase.kind === "checking" || phase.kind === "loading"}>
        <Body phase={phase} say={say} locale={locale} session={session.current} onRetry={start} catalogReady={catalogReady} />
      </main>
    </div>
  )
}

function stamp(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    timeZone: "UTC", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23",
  }).format(new Date(iso))
}

function Body({ phase, say, locale, session, onRetry, catalogReady }: {
  phase: Phase
  say: Say
  locale: string
  session: SessionHandle | null
  onRetry: () => void
  catalogReady: boolean
}) {
  switch (phase.kind) {
    case "misdeclared":
      return <Notice title={say("failedTitle")} body={say("misdeclared")} />
    case "checking":
    case "loading":
      return <Skeleton label={say(phase.kind === "checking" ? "checking" : "loading")} />
    case "sign_in":
      return (
        <Notice title={say("signInTitle")} body={say("signInBody")}>
          <a className="adm-btn adm-btn-go" href={phase.url}>{say("signIn")}</a>
        </Notice>
      )
    case "device_limit":
      if (!catalogReady) return <Skeleton label={say("loading")} />
      return (
        <section className="adm-notice adm-notice-wide">
          <DeviceLimitPanel session={session} tier={phase.tier} limit={phase.limit} onContinue={onRetry} signInLabel={say("deviceLimitSignIn")} />
        </section>
      )
    case "session_failed":
      return (
        <Notice title={say("sessionFailedTitle")} body={say("failedBody")} code={say("failedCode", { code: phase.code })}>
          <button type="button" className="adm-btn adm-btn-go" onClick={onRetry}>{say("tryAgain")}</button>
        </Notice>
      )
    case "forbidden":
      return (
        <Notice title={say("forbiddenTitle")} body={say("forbiddenBody")}>
          {session ? <a className="adm-btn" href={session.signInURL()}>{say("signIn")}</a> : null}
        </Notice>
      )
    case "failed":
      return (
        <Notice title={say("failedTitle")} body={say("failedBody")} code={say("failedCode", { code: phase.code })}>
          <button type="button" className="adm-btn adm-btn-go" onClick={onRetry}>{say("tryAgain")}</button>
        </Notice>
      )
    case "ready":
      return <AdminDashboard overview={phase.overview} say={say} locale={locale} stale={phase.stale} />
  }
}

function Notice({ title, body, code, children }: { title: string; body: string; code?: string; children?: React.ReactNode }) {
  const heading = useRef<HTMLHeadingElement>(null)
  // A state that replaces the page takes the focus, so a keyboard or a screen
  // reader lands on what changed rather than on a button that went away.
  useEffect(() => heading.current?.focus({ preventScroll: true }), [title])
  return (
    <section className="adm-notice">
      <h2 ref={heading} tabIndex={-1}>{title}</h2>
      <p>{body}</p>
      {code ? <p className="adm-code">{code}</p> : null}
      {children ? <div className="adm-notice-actions">{children}</div> : null}
    </section>
  )
}

function Skeleton({ label }: { label: string }) {
  return (
    <div className="adm-skeleton" role="status">
      <span className="adm-visually-hidden">{label}</span>
      <p className="adm-skeleton-say" aria-hidden="true">{label}</p>
      <div className="adm-tiles" aria-hidden="true">
        {[0, 1, 2].map((i) => <div key={i} className="adm-tile adm-shimmer" />)}
      </div>
      <div className="adm-panel adm-shimmer adm-shimmer-tall" aria-hidden="true" />
    </div>
  )
}

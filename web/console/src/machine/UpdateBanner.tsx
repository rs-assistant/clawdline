import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { UpdateStatus } from "@clawdline/contract"
import { catalogWordLanguage } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { followsRelay } from "../client.js"
import { applyUpdate, checkUpdateNow, currentUpdateRead, publishUpdateRead, refreshUpdate, subscribeUpdate } from "./update.js"
import { canRestartStagedApp, restartStagedApp } from "./restart-app.js"
import { INSTALL_COMMAND, UPDATE_BANNER_DISMISSED_KEY, buildName, canStartUpdateAfterFresh, devCommit, updateBannerVersion, updatePanel } from "./update-model.js"
import "./update.css"

function dismissedVersion(): string | null {
  try {
    return localStorage.getItem(UPDATE_BANNER_DISMISSED_KEY)
  } catch {
    return null
  }
}

/** The session list can start an update directly, then stay to show its result. */
export function UpdateBanner({ embedded = false }: { embedded?: boolean } = {}) {
  const read = useSyncExternalStore(subscribeUpdate, currentUpdateRead, currentUpdateRead)
  const [dismissed, setDismissed] = useState(dismissedVersion)
  const [target, setTarget] = useState("")
  const [sending, setSending] = useState(false)
  const [following, setFollowing] = useState(false)
  const [completed, setCompleted] = useState(false)
  const [dismissedResult, setDismissedResult] = useState(false)
  const [checkFailed, setCheckFailed] = useState(false)
  const [pressRefused, setPressRefused] = useState<{ code: string; detail: string } | null>(null)
  const [pressOlder, setPressOlder] = useState(false)
  const last = useRef<UpdateStatus | null>(null)
  if (read?.kind === "status") last.current = read.status

  const version = updateBannerVersion(read?.kind === "status" ? read.status : null, dismissed)
  const view = updatePanel({
    read, last: last.current, following, sending, pressRefused, pressOlder,
    overCloud: followsRelay(), autoSaving: false, autoFailed: "",
  }, nextWord, (at) => new Date(at).toLocaleString(document.documentElement.lang || undefined))
  const problem = target && (!version || version === target) ? view.problem : null

  useEffect(() => {
    if (completed && version && version !== target) setCompleted(false)
  }, [completed, version, target])

  useEffect(() => {
    if (!following) return
    const timer = setInterval(() => void refreshUpdate(), 2_000)
    return () => clearInterval(timer)
  }, [following])

  useEffect(() => {
    if (!following || read?.kind !== "status") return
    const apply = read.status.apply
    if (apply?.state === "healthy" || (read.status.state === "current" && buildName(read.status.running) === target)) {
      setFollowing(false)
      setCompleted(true)
    } else if (apply?.state === "failed" || apply?.state === "rolled_back") {
      setFollowing(false)
    }
  }, [following, read, target])

  const press = async () => {
    if (sending || following) return
    setSending(true)
    setCheckFailed(false)
    setPressRefused(null)
    setPressOlder(false)
    setCompleted(false)
    setDismissedResult(false)
    setTarget(version || buildName(last.current?.latest))

    // A banner may have been visible since before the release was withdrawn.
    // Recheck its signed source immediately before sending the install command.
    const checked = await checkUpdateNow()
    if (!canStartUpdateAfterFresh(checked, currentUpdateRead())) {
      setSending(false)
      setTarget("")
      if (checked.kind === "unreachable" || checked.kind === "refused") setCheckFailed(true)
      return
    }
    if (checked.kind !== "status") return
    setTarget(buildName(checked.status.latest))
    const answer = await applyUpdate()
    setSending(false)
    if (answer.kind === "started") {
      setFollowing(true)
      if (answer.status) publishUpdateRead({ kind: "status", status: answer.status })
      else void refreshUpdate()
    } else if (answer.kind === "older") {
      setPressOlder(true)
    } else if (answer.kind === "refused") {
      setPressRefused({ code: answer.code, detail: answer.detail })
    } else {
      // The daemon may already be restarting; its next status settles this.
      setFollowing(true)
      void refreshUpdate()
    }
  }

  const finishedVersion = last.current?.apply?.state === "healthy" ? last.current.apply.to : ""
  const visible = Boolean(version || sending || following || (!dismissedResult && (
    completed || checkFailed || pressRefused || pressOlder || problem || view.progress || (view.done && dismissed !== finishedVersion)
  )))
  if (!visible) return null
  const dismiss = () => {
    const dismissedRelease = version || finishedVersion
    if (dismissedRelease) {
      setDismissed(dismissedRelease)
      try { localStorage.setItem(UPDATE_BANNER_DISMISSED_KEY, dismissedRelease) } catch { /* current page only */ }
    }
    setCompleted(false)
    setDismissedResult(true)
    setCheckFailed(false)
    setPressRefused(null)
    setPressOlder(false)
  }
  const dev = read?.kind === "status" && read.status.install_kind === "release" ? devCommit(read.status.running) : null
  const canPress = read?.kind === "status" && (Boolean(version) || view.press.shown) && !sending && !following && !completed
  const busy = Boolean(sending || following || view.progress)
  const restartVersion = view.staged ? last.current?.apply?.to || "" : ""
  const message = checkFailed ? nextWord("updateIsUnknown")
    : dismissedResult ? null : problem?.sentence || view.progress || (sending ? nextWord("updateVerifying", { to: target }) : null)
      || (completed ? view.done || nextWord("updateHealthy", { to: target }) : null)
      || (view.done && dismissed !== finishedVersion ? view.done : null)

  return (
    <div className="update-banner" id={embedded ? undefined : "update-banner"} role="status" aria-busy={busy} data-version={version || target}>
      {/* The banner sits in the session list's column, which is narrow on a
          wide window: words above, actions below, never side by side. */}
      <div className="update-banner-body">
        {version && !sending && !following && !completed && !problem ? (
          <span className="update-banner-say" lang={catalogWordLanguage("next", "updateBanner")}>{dev
            ? nextWord("updateBannerDev", { commit: dev }) : nextWord("updateBanner", { version })}</span>
        ) : null}
        {message ? <span className="update-banner-say" aria-live="polite">
          {busy ? <span className="update-banner-spinner" aria-hidden="true" /> : null}
          {message}
        </span> : null}
        {view.staged ? <small className="update-banner-staged">{view.staged}</small> : null}
        {pressOlder ? <code className="update-command">{INSTALL_COMMAND}</code> : null}
      </div>
      {(canPress && !checkFailed) || (restartVersion && canRestartStagedApp()) ? (
        <div className="update-banner-actions">
          {canPress && !checkFailed ? (
            <button className="chip update-banner-go" type="button" onClick={() => void press()}>
              {view.press.label}
            </button>
          ) : null}
          {restartVersion && canRestartStagedApp() ? (
            <button className="chip update-banner-also" type="button" onClick={() => restartStagedApp(restartVersion)}>
              {nextWord("updateRestartApp")}
            </button>
          ) : null}
        </div>
      ) : null}
      {!sending && !following ? (
        <button className="x" type="button" aria-label={nextWord("updateBannerDismiss")} title={nextWord("updateBannerDismiss")} onClick={dismiss}>×</button>
      ) : null}
    </div>
  )
}

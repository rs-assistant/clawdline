import { useEffect, useRef, useSyncExternalStore } from "react"
import { nextWord, type NextWord } from "../../next-strings.js"
import { handover, type HandoverController } from "./handover.js"
import "../../pages/devices/account-machines.css"
import "./handover.css"

/*
 * The hosted page's half of the code comparison: the sheet a `#pairid=` link opens. It draws in
 * the machine sheet's own style (account-machines.css) so the handover reads as part of the
 * Devices sheet rather than a second product.
 */

const FAILURE_WORDS: Record<string, NextWord> = {
  not_confirmed: "handoverFailNotConfirmed",
  wrong_account: "handoverFailWrongAccount",
  poll_exhausted: "handoverFailExpired",
  unavailable: "handoverFailUnavailable",
}

function failureWords(code: string): string {
  const key = FAILURE_WORDS[code]
  return key ? nextWord(key) : nextWord("handoverFailOther", { code })
}

function grouped(code: string): string {
  return code.length === 6 ? code.slice(0, 3) + " " + code.slice(3) : code
}

export function IdentityHandoverDialog({ controller = handover }: { controller?: HandoverController }) {
  const state = useSyncExternalStore((l) => controller.watch(l), () => controller.state(), () => controller.state())
  const box = useRef<HTMLDivElement | null>(null)
  const open = state.phase !== "idle"
  useEffect(() => { if (open) box.current?.focus({ preventScroll: true }) }, [open])
  if (!open) return null

  const close = () => {
    if (state.phase === "comparing") controller.confirm(false)
    controller.stop()
  }

  return (
    <div className="am-layer" onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); close() } }}>
      <div className="am-scrim" onClick={state.phase === "working" ? undefined : close} aria-hidden="true" />
      <div className="am-sheet" role="dialog" aria-modal="true" aria-labelledby="handover-title" ref={box} tabIndex={-1}>
        <div className="am-grab" aria-hidden="true" />
        <div className="am-sheet-head">
          <div className="am-sheet-name">
            <h2 id="handover-title">{nextWord("handoverTitle")}</h2>
            <p className="am-meta">{nextWord("handoverLead")}</p>
          </div>
        </div>
        <div className="am-stack">
          {state.phase === "comparing" ? (
            <>
              <p className="am-say">{nextWord("handoverCompare")}</p>
              <p className="am-handover-code" aria-label={nextWord("handoverCodeLabel")}>{grouped(state.code)}</p>
              <p className="am-say am-sub">{nextWord("handoverMachineKey", { key: state.machineFingerprint })}</p>
              <div className="am-pair-buttons">
                <button type="button" className="am-accent" onClick={() => controller.confirm(true)}>{nextWord("handoverMatch")}</button>
                <button type="button" className="am-ghost" onClick={close}>{nextWord("handoverNoMatch")}</button>
              </div>
            </>
          ) : state.phase === "working" ? (
            <>
              <p className="am-say" role="status">{nextWord("handoverWorking")}</p>
              <div className="am-pair-buttons">
                <button type="button" className="am-ghost" onClick={close}>{nextWord("handoverCancel")}</button>
              </div>
            </>
          ) : state.phase === "done" ? (
            <>
              <p className="am-say" role="status">{nextWord("handoverDone", { epoch: state.contentKeyEpoch })}</p>
              <div className="am-pair-buttons">
                <button type="button" className="am-primary" onClick={close}>{nextWord("handoverClose")}</button>
              </div>
            </>
          ) : (
            <>
              <p className="am-say" role="alert">{failureWords(state.code)}</p>
              <div className="am-pair-buttons">
                <button type="button" className="am-primary" onClick={close}>{nextWord("handoverClose")}</button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

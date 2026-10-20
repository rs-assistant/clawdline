import { useEffect, useLayoutEffect, useRef } from "react"

/** Measure only on toggles and retain native content until the exit completes.
 * The summary state and inert body follow the requested state immediately. */
export function useDetailsMotion(open: boolean, identity: string) {
  const root = useRef<HTMLDetailsElement>(null)
  const running = useRef<Animation | null>(null)
  useLayoutEffect(() => {
    const details = root.current
    const body = details?.querySelector<HTMLElement>(".session-todos-body")
    if (!details || !body) { running.current?.cancel(); running.current = null; return }
    const before = getComputedStyle(body)
    const height = details.open ? before.height : "0px"
    const padding = details.open ? before.paddingBottom : "0px"
    running.current?.cancel()
    running.current = null
    if (!details.open && !open) return
    details.open = true
    const after = getComputedStyle(body)
    const target = open ? after.height : "0px"
    const targetPadding = open ? after.paddingBottom : "0px"
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches || typeof body.animate !== "function") {
      details.open = open
      return
    }
    const animation = body.animate([
      { height, paddingBottom: padding },
      { height: target, paddingBottom: targetPadding },
    ], { duration: 220, easing: "cubic-bezier(.2, .7, .2, 1)", fill: "both" })
    running.current = animation
    animation.onfinish = () => {
      if (running.current !== animation) return
      if (!open) details.open = false
      running.current = null
      animation.cancel()
    }
  }, [open, identity])
  useEffect(() => () => running.current?.cancel(), [])
  return root
}

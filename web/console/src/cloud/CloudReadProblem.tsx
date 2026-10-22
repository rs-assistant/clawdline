import { useState } from "react"

/** A quiet status row never covers content or takes focus from a draft. */
export function CloudReadProblem(props: {
  message: string; code: string; trace?: string; retryLabel: string; dismissLabel: string;
  onRetry: () => void;
}) {
  const [dismissed, setDismissed] = useState(false)
  if (dismissed) return null
  return <section role="status" className="cloud-read-problem">
    <span title={props.code + (props.trace ? ` · ${props.trace}` : "")}>{props.message}</span>
    <button type="button" onClick={props.onRetry}>{props.retryLabel}</button>
    <button type="button" aria-label={props.dismissLabel} title={props.dismissLabel}
      onClick={() => setDismissed(true)}>×</button>
  </section>
}

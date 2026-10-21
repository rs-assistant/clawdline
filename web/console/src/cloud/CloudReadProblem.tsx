/** An error keeps the existing console mounted, including unsent drafts. */
export function CloudReadProblem(props: {
  message: string; code: string; trace?: string; pairLabel: string; retryLabel: string;
  onPair?: () => void; onRetry: () => void;
}) {
  return <section role="alert" className="cloud-read-problem">
    <p>{props.message}</p>
    <p><code>{props.code}{props.trace ? ` · ${props.trace}` : ""}</code></p>
    {props.onPair && <button className="chip" type="button" onClick={props.onPair}>{props.pairLabel}</button>}
    <button className="chip" type="button" onClick={props.onRetry}>{props.retryLabel}</button>
  </section>
}

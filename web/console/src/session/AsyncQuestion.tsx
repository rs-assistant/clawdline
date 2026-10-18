import { useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { catalogWord } from "../catalog.js"
import * as L from "../legacy/bridge.js"
import { deliverUntilSeen, pendingSends } from "./send.js"
import { pendingFailureSentence } from "./pending-copy.js"
import "./async-question.css"

interface Question {
  header: string
  text: string
  multi: boolean
  options: { label: string; note: string }[]
}

/** Async questions receive conversation messages, not terminal picker keys. */
export function AsyncQuestion({ sessionID, questions }: { sessionID: string; questions: Question[] }) {
  const sending = useRef(false)
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState<Record<number, boolean>>({})
  const [status, setStatus] = useState("")
  const reply = async (q: Question, index: number, label: string) => {
    if (sending.current || sent[index]) return
    sending.current = true
    setBusy(true)
    setSent((was) => ({ ...was, [index]: true }))
    setStatus("")
    try {
      const text = `${q.text}\n\n${label}`
      const card = pendingSends.add(sessionID, text, [], Date.now())
      const code = await deliverUntilSeen(card)
      if (code) setStatus(pendingFailureSentence(code))
      else {
        setSent((was) => ({ ...was, [index]: true }))
        setStatus(catalogWord("literal", "cc8e34d543a9"))
      }
    } finally {
      sending.current = false
      setBusy(false)
    }
  }
  return <div className="body"><div className="askbox async-question">
    <div className="asktag">{L.strings.webAskLabel}</div>
    {questions.map((q, index) => <div key={index}>
      {q.header && <div className="askhead">{q.header}</div>}
      <div className="askq" dangerouslySetInnerHTML={{ __html: L.inlineMdHTML(q.text) }} />
      <ol className="askopts">{q.options.map((option, n) => <li key={n}>
        <button type="button" className="askopt async-question-option" disabled={busy || !!sent[index]}
          onClick={() => { void reply(q, index, option.label) }}>
          <span className="n">{n + 1}</span><span className="what">
            <b>{option.label}</b>{option.note && <span className="note">{option.note}</span>}
          </span>
        </button>
      </li>)}</ol>
    </div>)}
    <div className="askany">{nextWord("codexAsyncChoiceHint")}</div>
    {status && <div role="status">{status}</div>}
  </div></div>
}

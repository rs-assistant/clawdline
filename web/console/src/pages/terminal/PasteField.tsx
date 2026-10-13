import { useState } from "react"
import { nextWord } from "../../next-strings.js"

/** Native paste target for browsers that refuse programmatic clipboard reads. */
export function PasteField({ available, send, close }: {
  available: boolean
  send: (text: string) => Promise<boolean> | boolean
  close: () => void
}) {
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  return <div className="terminal-paste-field">
    <label>{nextWord("terminalPasteFieldHelp")}
      <textarea autoFocus value={text} onChange={event => setText(event.target.value)}
        autoCapitalize="off" autoComplete="off" spellCheck={false} rows={3} />
    </label>
    <div className="terminal-paste-actions">
      <button className="board-button" type="button" disabled={!available || !text || busy} onClick={async () => {
        setBusy(true)
        try { if (await send(text)) close() } finally { setBusy(false) }
      }}>{nextWord("terminalPasteSend")}</button>
      <button className="board-button" type="button" onClick={close}>{nextWord("terminalPasteCancel")}</button>
    </div>
  </div>
}

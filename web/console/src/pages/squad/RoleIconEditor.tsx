import { useState } from "react"
import type { Icon } from "@clawdline/contract"
import { nextWord } from "../../next-strings.js"
import { failureSentence } from "../../refusals/refusal-text.js"
import type { SquadAPI } from "./api.js"

export function freshRoleIcon(): Icon {
  const palettes = [["#e58a35", "#4a6970", "#f3d59b"], ["#42a89b", "#28576b", "#f0c875"], ["#cc6654", "#594d43", "#ecd8ad"]]
  const colors = palettes[crypto.getRandomValues(new Uint8Array(1))[0] % palettes.length]
  const rows = ["..1111..", ".122221.", "12322321", "12222221", "12333321", ".122221.", "..1..1.."]
  return { accent: colors[0], cells: rows.map((row) => [...row].map((cell) => cell === "." ? null : colors[Number(cell) - 1])) }
}

export function RoleIconEditor({ icon, onChange, disabled }: { icon: Icon; onChange: (icon: Icon) => void; disabled: boolean }) {
  const [color, setColor] = useState("#e58a35")
  const [erase, setErase] = useState(false)
  return <fieldset className="squad-icon-editor" disabled={disabled}>
    <legend>{nextWord("roleIconTitle")}</legend>
    <label>{nextWord("roleIconColor")} <input type="color" value={color} onChange={(event) => { setColor(event.target.value); setErase(false) }} /></label>
    <button type="button" aria-pressed={erase} onClick={() => setErase(!erase)}>{nextWord("roleIconErase")}</button>
    <button type="button" onClick={() => onChange(freshRoleIcon())}>{nextWord("roleIconVariant")}</button>
    <div className="squad-icon-pixels">{icon.cells.flatMap((row, y) => row.map((cell, x) => <button key={`${x}-${y}`} type="button"
      aria-label={`${nextWord("roleIconPixel")} ${x + 1}, ${y + 1}`} style={{ backgroundColor: cell ?? "transparent" }}
      onClick={() => onChange({ ...icon, accent: erase ? icon.accent : color, cells: icon.cells.map((line, r) => line.map((value, c) => r === y && c === x ? erase ? null : color : value)) })} />))}</div>
  </fieldset>
}

export function RoleIconUpdate({ api, id, icon, canWrite, onSaved }: { api: SquadAPI; id: string; icon: Icon; canWrite: boolean; onSaved: () => Promise<void> }) {
  const [value, setValue] = useState(icon)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [pending, setPending] = useState<{ body: Parameters<SquadAPI["createPersona"]>[0]; version: number; key: string } | null>(null)
  const save = async () => {
    if (busy) return
    setBusy(true); setError("")
    try {
      let request = pending
      if (!request) {
        const catalog = await api.readCatalog()
        const current = catalog.definitions.find((row) => row.definition_id === id)
        if (!current || id.startsWith("clawdline.")) throw new Error("unknown_definition")
        // Select the published fields explicitly: a read's digest belongs to
        // its old content, while all pinned references must survive this edit.
        request = { body: { definition_id: id, version: `icon-${crypto.randomUUID()}`, source: current.source, license: current.license,
          name: current.name, summary: current.summary, body: current.body, teams: current.teams, skills: current.skills, icon: value },
          version: catalog.catalog_version, key: crypto.randomUUID() }
        setPending(request)
      }
      await api.createPersona(request.body, request.version, request.key)
      setPending(null)
      await onSaved()
    } catch (cause) { setError(failureSentence(cause, nextWord("roleCreateFailed"))) }
    finally { setBusy(false) }
  }
  return <details><summary>{nextWord("roleIconEdit")}</summary>
    <RoleIconEditor icon={value} disabled={!canWrite || busy || !!pending} onChange={setValue} />
    {error && <p role="alert">{error}</p>}
    <button type="button" disabled={!canWrite || busy} onClick={() => void save()}>{nextWord("roleIconSave")}</button>
    {pending && <button type="button" disabled={busy} onClick={() => { setPending(null); setValue(icon); setError(""); void onSaved() }}>{nextWord("roleIconReload")}</button>}
  </details>
}

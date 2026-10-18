import { useEffect, useRef, useState } from "react"
import { catalogWord } from "../../catalog.js"
import { showModalWithoutChoice } from "../../modal-focus.js"
import { failureSentence } from "../../legacy/bridge.js"
import { addProjectPlace, readProjectFolders, type ProjectFolderPage } from "../work/api.js"
import "./project-folder-picker.css"

const word = (key: string) => catalogWord("projects", key)

/** Browse directories on the selected machine, then register one existing folder. */
export function ProjectFolderPicker({ open, machine, onClose, onAdded }: {
  open: boolean
  machine?: string
  onClose: () => void
  onAdded: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const browseGeneration = useRef(0)
  const [page, setPage] = useState<ProjectFolderPage | null>(null)
  const [typed, setTyped] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  async function browse(path?: string) {
    const generation = ++browseGeneration.current
    setBusy(true); setError("")
    try {
      const result = await readProjectFolders(path, machine)
      if (generation !== browseGeneration.current) return
      setPage(result)
      setTyped(result.path)
    } catch (cause) {
      if (generation === browseGeneration.current) setError(failureSentence(cause, word("folderReadFailed")))
    } finally { if (generation === browseGeneration.current) setBusy(false) }
  }

  useEffect(() => {
    const node = dialog.current
    if (!node) return
    if (open) {
      showModalWithoutChoice(node)
      setPage(null); setTyped(""); setError("")
      void browse()
    } else {
      browseGeneration.current++
      if (node.open) node.close()
    }
  }, [open, machine])

  async function add() {
    if (!page || busy) return
    setBusy(true); setError("")
    try {
      await addProjectPlace(page.path, machine)
      onAdded()
      onClose()
    } catch (cause) {
      setError(failureSentence(cause, word("folderAddFailed")))
    } finally { setBusy(false) }
  }

  return <dialog ref={dialog} className="project-folder-picker"
    aria-labelledby="project-folder-picker-title"
    onCancel={event => { if (busy) event.preventDefault(); else onClose() }}
    onClose={onClose}>
    <h2 id="project-folder-picker-title">{word("folderTitle")}</h2>
    <p>{word("folderHint")}</p>
    <form onSubmit={event => { event.preventDefault(); void browse(typed) }}>
      <label htmlFor="project-folder-path">{word("folderPath")}</label>
      <div className="project-folder-path-row">
        <input id="project-folder-path" value={typed} onChange={event => setTyped(event.target.value)}
          disabled={busy} autoComplete="off" spellCheck={false} />
        <button type="submit" disabled={busy || !typed}>{word("folderGo")}</button>
      </div>
    </form>
    {page && <>
      <p className="project-folder-current">{page.path}</p>
      <div className="project-folder-list" role="group" aria-label={word("folderList")}>
        {page.parent && <button type="button" disabled={busy} onClick={() => void browse(page.parent)}>{word("folderParent")}</button>}
        {page.folders.map(folder => <button type="button" key={folder.path} disabled={busy}
          onClick={() => void browse(folder.path)}>📁 {folder.name}</button>)}
        {!page.folders.length && <p>{word("folderEmpty")}</p>}
      </div>
      {page.truncated && <p role="status">{word("folderTruncated")}</p>}
    </>}
    {error && <p role="alert">{error}</p>}
    <div className="project-folder-actions">
      <button type="button" disabled={busy} onClick={onClose}>{word("folderCancel")}</button>
      <button type="button" disabled={busy || !page} onClick={() => void add()}>{word("folderAdd")}</button>
    </div>
  </dialog>
}

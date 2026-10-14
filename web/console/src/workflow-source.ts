import type { WorkflowSource } from "./legacy/snippets-bridge.js"
import type { ProjectFile } from "./pages/projects/project-files-api.js"
import { workflowWordFor } from "./workflow-words.js"

type Language = "en" | "zh-Hant"
const commandName = (file: ProjectFile) => file.name.replace(/\.md$/i, "")

export function workflowSourceTitle(file: ProjectFile): string {
  return file.kind === "command" && (!file.title || file.title === commandName(file))
    ? commandName(file).replace(/[-_]+/g, " ").replace(/\b[a-z]/g, (letter) => letter.toUpperCase())
    : file.title || file.location.split("/").filter(Boolean).at(-2) || file.name
}

export function workflowSourceText(file: ProjectFile, language: Language): { title: string; body: string } {
  const title = workflowSourceTitle(file)
  return { title, body: file.kind === "command"
    ? workflowWordFor(language, "commandPrompt", { name: commandName(file) })
    : workflowWordFor(language, "skillPrompt", { name: title }) }
}

export function workflowSourceMatches(saved: { title: string; body: string; source: WorkflowSource }, file: ProjectFile): boolean {
  if (file.id !== saved.source.file_id || file.kind !== saved.source.kind || file.status !== "ready") return false
  const current = workflowSourceTitle(file)
  if (saved.title !== current || saved.source.name !== current) return false
  return (["en", "zh-Hant"] as const).some((language) => saved.body === workflowSourceText(file, language).body)
}

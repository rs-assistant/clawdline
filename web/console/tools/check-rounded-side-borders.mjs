import { readdir, readFile } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"
import postcss from "postcss"

const source = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../src")
const pinned = JSON.parse(await readFile(path.join(source, "legacy/MANIFEST.json"), "utf8"))
const pinnedCSS = new Set(Object.keys(pinned.css).map((name) => path.join(source, "legacy", name)))
const css = []
async function collect(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name)
    if (entry.isDirectory()) await collect(file)
    else if (entry.name.endsWith(".css")) css.push(file)
  }
}
await collect(source)
if (!css.length) throw new Error(`No CSS files found in ${source}`)

const violations = []
for (const file of css) {
  // The copied CSS is pinned by check-legacy-css.sh. The active stylesheet
  // below replaces its old asymmetric card borders without changing history.
  if (pinnedCSS.has(file)) continue
  const root = postcss.parse(await readFile(file, "utf8"), { from: file })
  root.walkRules((rule) => {
    const declarations = rule.nodes.filter((node) => node.type === "decl")
    const has = (name) => declarations.some((node) => name.test(node.prop) && !/^(0(?:px)?|none|initial|unset)$/i.test(node.value.trim()))
    // This one pseudo-element is a tree connector, not a rounded surface.
    if (rule.selector === ".bg-sheet .agents .one.child::before") return
    if (has(/^border-(left|inline-start)(?:-|$)/) && has(/^border(?:-[a-z]+)*-radius$/)) {
      violations.push(`${path.relative(process.cwd(), file)}:${rule.source.start.line} ${rule.selector}`)
    }
  })
}
const overrides = path.join(source, "side-border-overrides.css")
const overrideRoot = postcss.parse(await readFile(overrides, "utf8"), { from: overrides })
const uniformLegacyCards = new Set([
  ".composer .waiting", ".peer-card", ".message-card", ".askbox", ".notice-card",
])
overrideRoot.walkRules((rule) => {
  if (!rule.nodes.some((node) => node.type === "decl" && node.prop === "border-left" && /^1px solid /.test(node.value))) return
  for (const selector of rule.selectors) uniformLegacyCards.delete(selector)
})
if (uniformLegacyCards.size) violations.push(`Missing uniform overrides for pinned CSS: ${[...uniformLegacyCards].join(", ")}`)
const main = await readFile(path.join(source, "main.tsx"), "utf8")
if (!main.includes('import "./side-border-overrides.css"')) violations.push("main.tsx does not load the pinned CSS overrides")
if (violations.length) {
  console.error("Rounded surfaces with a left accent border are forbidden:\n" + violations.join("\n"))
  process.exitCode = 1
} else {
  console.log(`Checked ${css.length} CSS files: no rounded side-border surfaces`)
}

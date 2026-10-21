// Compare current domain metadata with the display manifest and primary catalogs.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

export function checkBuiltinDisplay(root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..")) {
  const read = file => fs.readFileSync(path.join(root, file), "utf8")
  const expected = { en: {}, "zh-Hant": {} }
  const add = (domain, id, field, en, zh) => {
    const key = `${domain}.${id}.${field}`
    if (Object.hasOwn(expected.en, key)) throw new Error(`duplicate builtin display key: ${key}`)
    expected.en[key] = en
    expected["zh-Hant"][key] = zh
  }
  const directory = "internal/domain/persona/catalog"
  const files = fs.readdirSync(path.join(root, directory)).filter(file => file.endsWith(".md"))
  if (!files.length) throw new Error("no builtin persona sources")
  for (const file of files) {
    const front = /^---\r?\n([\s\S]*?)\r?\n---/.exec(read(`${directory}/${file}`))?.[1]
    if (!front) throw new Error(`missing persona frontmatter: ${file}`)
    const field = name => {
      const value = new RegExp(`^${name}: (.+)$`, "m").exec(front)?.[1]?.trim()
      if (!value) throw new Error(`missing persona ${name}: ${file}`)
      return value
    }
    for (const name of ["name", "summary"]) add("builtinPersona", field("id"), name, field(`${name}_en`), field(`${name}_zh`))
  }
  const skills = /var builtinSkillSpecs = \[\]builtinSkillSpec\{([\s\S]*?)\n\}/.exec(read("internal/domain/squad/builtin_skills.go"))?.[1]
  if (!skills) throw new Error("missing builtin skill source table")
  const rows = skills.split("\n").map(line => line.trim()).filter(line => line.startsWith("{"))
  if (!rows.length) throw new Error("no builtin skill sources")
  for (const row of rows) {
    // Only the first five fields are display metadata; Source can be a Go constant.
    const strings = [...row.matchAll(/"(?:[^"\\]|\\.)*"/g)].slice(0, 5).map(match => JSON.parse(match[0]))
    if (strings.length !== 5) throw new Error("unreadable builtin skill metadata")
    const [, id, zh, purpose, zhPurpose] = strings
    add("builtinSkill", id, "name", id, zh)
    add("builtinSkill", id, "purpose", purpose, zhPurpose)
  }
  const manifest = JSON.parse(read("web/console/src/builtin-display-source.json"))
  const errors = []
  for (const tag of ["en", "zh-Hant"]) {
    const catalog = JSON.parse(read(`web/console/public/catalogs/${tag}.json`))
    const catalogKeys = Object.keys(catalog).filter(key => key.startsWith("builtinPersona.") || key.startsWith("builtinSkill."))
    for (const key of new Set([...Object.keys(expected[tag]), ...Object.keys(manifest[tag] ?? {}), ...catalogKeys])) {
      if (manifest[tag]?.[key] !== expected[tag][key]) errors.push(`${key}: ${tag} builtin source/manifest mismatch`)
      if (catalog[key] !== expected[tag][key]) errors.push(`${key}: ${tag} builtin source/catalog mismatch`)
    }
  }
  if (errors.length) throw new Error(errors.join("\n"))
  return Object.keys(expected.en).length
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { console.log(`builtin display: ${checkBuiltinDisplay(process.argv[2])} keys match domain sources and primary catalogs`) }
  catch (error) { console.error(error.message); process.exitCode = 1 }
}

/*
 * The plaintext inside an owner-sealed attachment: `clawdline.health-owner-diagnostics/1`,
 * the same whitelist `internal/adapters/healthsealed/snapshot.go` writes. Where the machine
 * was stuck, for how long, against which deadline, on which build and stage, under which
 * correlation id, and what the evidence covers. There is no field for a payload, a Session,
 * a document, terminal or browser content, and an unknown field is refused.
 *
 * Nanosecond counts can exceed 2^53, so every int64 stays a decimal string here; it is never
 * turned into a Number. A missing count is `null` (unknown), never 0.
 */
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { SealedError } from "./crypto.ts"

export const SNAPSHOT_SCHEMA = "clawdline.health-owner-diagnostics/1"
/** The Go writer's bounds (`MaxSnapshotBytes`, `MaxSnapshotFieldChars`, `MaxSnapshotCoverage`). */
export const SNAPSHOT_MAX_BYTES = 16 * 1024
export const SNAPSHOT_FIELD_MAX_CHARS = 512
export const SNAPSHOT_COVERAGE_MAX = 32

/** A signed 64-bit integer as exact decimal text, or null when the machine could not say. */
export type Int64Text = string | null

export interface SnapshotCoverage {
  source: string
  fromMs: Int64Text
  toMs: Int64Text
  lost: Int64Text
  truncated: boolean
}

export interface OwnerSnapshot {
  schema: typeof SNAPSHOT_SCHEMA
  location: string
  ageNs: Int64Text
  deadlineNs: Int64Text
  build: string
  stage: string
  correlationID: string
  observedAtMs: Int64Text
  coverage: SnapshotCoverage[]
  truncated: boolean
  lost: Int64Text
}

const TOP = ["schema", "location", "age_ns", "deadline_ns", "build", "stage", "correlation_id", "observed_at_ms", "coverage", "truncated", "lost"]
const COVERAGE = ["source", "from_ms", "to_ms", "lost", "truncated"]
const INT64_MIN = -(2n ** 63n)
const INT64_MAX = 2n ** 63n - 1n

function bad(message: string): never {
  throw new SealedError("snapshot_invalid", message)
}

function object(value: unknown, keys: string[], what: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) bad(what + " is not an object")
  const record = value as Record<string, unknown>
  for (const key of Object.keys(record)) if (!keys.includes(key)) bad(what + " carries a field outside the whitelist")
  return record
}

function text(value: unknown, field: string): string {
  if (value === undefined) return ""
  if (typeof value !== "string" || [...value].length > SNAPSHOT_FIELD_MAX_CHARS) bad(field + " is not bounded text")
  return value
}

function int64(value: unknown, field: string): Int64Text {
  if (value === undefined || value === null) return null
  let digits: string
  if (typeof value === "string" && /^-?(0|[1-9][0-9]{0,18})$/.test(value)) digits = value
  else if (typeof value === "number" && Number.isSafeInteger(value)) digits = String(value)
  else bad(field + " is not an int64")
  const n = BigInt(digits)
  if (n < INT64_MIN || n > INT64_MAX) bad(field + " is outside int64")
  return digits
}

function flag(value: unknown, field: string): boolean {
  if (value === undefined) return false
  if (typeof value !== "boolean") bad(field + " is not a boolean")
  return value
}

/** Decodes opened plaintext, or throws `snapshot_invalid`; nothing partial comes back. */
export function decodeSnapshot(plaintext: Uint8Array): OwnerSnapshot {
  if (plaintext.length > SNAPSHOT_MAX_BYTES) bad("the snapshot is larger than its bound")
  let parsed: unknown
  try {
    parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(plaintext))
  } catch {
    bad("the snapshot is not UTF-8 JSON")
  }
  const r = object(parsed, TOP, "snapshot")
  if (r.schema !== SNAPSHOT_SCHEMA) bad("the snapshot schema is not " + SNAPSHOT_SCHEMA)
  const coverage = r.coverage === undefined ? [] : r.coverage
  if (!Array.isArray(coverage) || coverage.length > SNAPSHOT_COVERAGE_MAX) bad("coverage is not a bounded list")
  return {
    schema: SNAPSHOT_SCHEMA,
    location: text(r.location, "location"),
    ageNs: int64(r.age_ns, "age_ns"),
    deadlineNs: int64(r.deadline_ns, "deadline_ns"),
    build: text(r.build, "build"),
    stage: text(r.stage, "stage"),
    correlationID: text(r.correlation_id, "correlation_id"),
    observedAtMs: int64(r.observed_at_ms, "observed_at_ms"),
    coverage: coverage.map((entry, index) => {
      const c = object(entry, COVERAGE, "coverage " + index)
      return {
        source: text(c.source, "coverage.source"), fromMs: int64(c.from_ms, "coverage.from_ms"),
        toMs: int64(c.to_ms, "coverage.to_ms"), lost: int64(c.lost, "coverage.lost"),
        truncated: flag(c.truncated, "coverage.truncated"),
      }
    }),
    truncated: flag(r.truncated, "truncated"),
    lost: int64(r.lost, "lost"),
  }
}

/** Where the evidence has holes the person should know about before trusting it. */
export function evidenceGaps(s: OwnerSnapshot): string[] {
  const gaps: string[] = []
  if (s.truncated) gaps.push("truncated")
  if (s.lost === null) gaps.push("lost_unknown")
  else if (s.lost !== "0") gaps.push("lost")
  if (s.coverage.length === 0) gaps.push("no_coverage")
  if (s.coverage.some((c) => c.truncated)) gaps.push("coverage_truncated")
  if (s.coverage.some((c) => c.lost === null || c.lost !== "0")) gaps.push("coverage_lost")
  return gaps
}

export interface SnapshotExportSource {
  attachmentID: string
  machineID: string
  revision: number
  bootID: string
  subjectKind: string
  subjectID: string
  contentKeyEpoch: number
  expiresAtMs: number
  receivedAtMs: number
}

/**
 * The structured record a person may hand to an agent: the whitelisted snapshot and where it
 * came from, nothing else — no key, nonce, ciphertext or proof. Called only from a button.
 */
export function exportSnapshot(snapshot: OwnerSnapshot, source: SnapshotExportSource): string {
  return JSON.stringify({
    schema: "clawdline.health-owner-diagnostics-export/1",
    source: {
      attachment_id: source.attachmentID, machine_id: source.machineID, revision: source.revision,
      boot_id: source.bootID, subject_kind: source.subjectKind, subject_id: source.subjectID,
      content_key_epoch: source.contentKeyEpoch, expires_at_ms: source.expiresAtMs, received_at_ms: source.receivedAtMs,
    },
    snapshot: {
      schema: snapshot.schema, location: snapshot.location, age_ns: snapshot.ageNs, deadline_ns: snapshot.deadlineNs,
      build: snapshot.build, stage: snapshot.stage, correlation_id: snapshot.correlationID,
      observed_at_ms: snapshot.observedAtMs, truncated: snapshot.truncated, lost: snapshot.lost,
      coverage: snapshot.coverage.map((c) => ({ source: c.source, from_ms: c.fromMs, to_ms: c.toMs, lost: c.lost, truncated: c.truncated })),
    },
    evidence_gaps: evidenceGaps(snapshot),
  }, null, 2)
}

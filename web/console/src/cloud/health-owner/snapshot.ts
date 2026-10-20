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
/**
 * /1 plus the sanitized stall capture and the stall's recovery. The Go writer uses it only for a
 * revision that carries either; /1 may not carry them and /2 must carry one.
 */
export const SNAPSHOT_SCHEMA_V2 = "clawdline.health-owner-diagnostics/2"
/** The Go writer's bounds (`MaxSnapshotBytes`, `MaxSnapshotFieldChars`, `MaxSnapshotCoverage`). */
export const SNAPSHOT_MAX_BYTES = 16 * 1024
export const SNAPSHOT_FIELD_MAX_CHARS = 512
export const SNAPSHOT_COVERAGE_MAX = 32
/** `MaxSnapshotFrames` and `MaxSnapshotSources`. */
export const SNAPSHOT_FRAMES_MAX = 24
export const SNAPSHOT_SOURCES_MAX = 8

/** A signed 64-bit integer as exact decimal text, or null when the machine could not say. */
export type Int64Text = string | null

export interface SnapshotCoverage {
  source: string
  fromMs: Int64Text
  toMs: Int64Text
  lost: Int64Text
  truncated: boolean
}

export interface CaptureFrame {
  function: string
  source: string
  sourceKind: "module" | "std" | "dependency" | "unavailable"
}

/** The owner's part of a local stall capture; never the raw dump. */
export interface CaptureExcerpt {
  seq: Int64Text
  durationNs: Int64Text
  overBudget: boolean
  function: string
  mapped: boolean
  matchedGoroutines: Int64Text
  sources: string[]
  state: string
  frames: CaptureFrame[]
  framesOmitted: Int64Text
  goroutinesTotal: Int64Text
  rawTruncated: boolean
  truncated: boolean
  redacted: Int64Text
  sourceUnavailable: Int64Text
}

export interface StallRecovery {
  state: "recovered" | "released_outcome_unknown"
  recoveryNs: Int64Text
}

export interface OwnerSnapshot {
  schema: typeof SNAPSHOT_SCHEMA | typeof SNAPSHOT_SCHEMA_V2
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
  /** Only in /2; null in /1 and when the revision carries none. */
  capture: CaptureExcerpt | null
  recovery: StallRecovery | null
}

const TOP = ["schema", "location", "age_ns", "deadline_ns", "build", "stage", "correlation_id", "observed_at_ms", "coverage", "truncated", "lost"]
const TOP_V2 = [...TOP, "capture", "recovery"]
const COVERAGE = ["source", "from_ms", "to_ms", "lost", "truncated"]
const CAPTURE = ["seq", "duration_ns", "over_budget", "function", "mapped", "matched_goroutines", "sources", "state", "frames",
  "frames_omitted", "goroutines_total", "raw_truncated", "truncated", "functions_redacted", "source_unavailable"]
const FRAME = ["function", "source", "source_kind"]
const SOURCE_KINDS = ["module", "std", "dependency", "unavailable"]
const RECOVERY = ["state", "recovery_ns"]
/** The Go writer's `safeFunction` and `safeSource`: no argument list, address, absolute or climbing path. */
const SAFE_FUNCTION = /^[A-Za-z0-9_./*()[\]{},-]{1,200}$/
const SAFE_SOURCE = /^[A-Za-z0-9_./@!+-]{1,200}:[0-9]+$/
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

function functionName(value: unknown, field: string): string {
  const name = text(value, field)
  if (name !== "" && name !== "<redacted>" && !SAFE_FUNCTION.test(name)) bad(field + " is not an allowlisted function")
  return name
}

function source(value: unknown, field: string): string {
  const s = text(value, field)
  if (s !== "" && (!SAFE_SOURCE.test(s) || s.startsWith("/") || s.includes(".."))) bad(field + " is not a relative source line")
  return s
}

function decodeCapture(value: unknown): CaptureExcerpt | null {
  if (value === undefined || value === null) return null
  const c = object(value, CAPTURE, "capture")
  const sources = c.sources === undefined ? [] : c.sources
  const frames = c.frames === undefined ? [] : c.frames
  if (!Array.isArray(sources) || sources.length > SNAPSHOT_SOURCES_MAX) bad("capture.sources is not a bounded list")
  if (!Array.isArray(frames) || frames.length > SNAPSHOT_FRAMES_MAX) bad("capture.frames is not a bounded list")
  return {
    seq: int64(c.seq, "capture.seq"), durationNs: int64(c.duration_ns, "capture.duration_ns"),
    overBudget: flag(c.over_budget, "capture.over_budget"), function: functionName(c.function, "capture.function"),
    mapped: flag(c.mapped, "capture.mapped"), matchedGoroutines: int64(c.matched_goroutines, "capture.matched_goroutines"),
    sources: sources.map((s) => {
      const line = source(s, "capture.sources")
      if (line === "") bad("capture.sources holds an empty line")
      return line
    }),
    state: text(c.state, "capture.state"),
    frames: frames.map((entry, index) => {
      const f = object(entry, FRAME, "capture frame " + index)
      const kind = text(f.source_kind, "capture.frames.source_kind")
      if (!SOURCE_KINDS.includes(kind)) bad("capture.frames.source_kind is not known")
      return { function: functionName(f.function, "capture.frames.function"), source: source(f.source, "capture.frames.source"),
        sourceKind: kind as CaptureFrame["sourceKind"] }
    }),
    framesOmitted: int64(c.frames_omitted, "capture.frames_omitted"), goroutinesTotal: int64(c.goroutines_total, "capture.goroutines_total"),
    rawTruncated: flag(c.raw_truncated, "capture.raw_truncated"), truncated: flag(c.truncated, "capture.truncated"),
    redacted: int64(c.functions_redacted, "capture.functions_redacted"), sourceUnavailable: int64(c.source_unavailable, "capture.source_unavailable"),
  }
}

function decodeRecovery(value: unknown): StallRecovery | null {
  if (value === undefined || value === null) return null
  const r = object(value, RECOVERY, "recovery")
  if (r.state !== "recovered" && r.state !== "released_outcome_unknown") bad("recovery.state is not known")
  return { state: r.state, recoveryNs: int64(r.recovery_ns, "recovery.recovery_ns") }
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
  const schema = (parsed as Record<string, unknown> | null)?.schema
  if (schema !== SNAPSHOT_SCHEMA && schema !== SNAPSHOT_SCHEMA_V2) bad("the snapshot schema is not " + SNAPSHOT_SCHEMA + " or " + SNAPSHOT_SCHEMA_V2)
  // /1 knows no capture or recovery: a /1 that carries one is refused, not half-read.
  const r = object(parsed, schema === SNAPSHOT_SCHEMA_V2 ? TOP_V2 : TOP, "snapshot")
  const capture = decodeCapture(r.capture)
  const recovery = decodeRecovery(r.recovery)
  if (schema === SNAPSHOT_SCHEMA_V2 && !capture && !recovery) bad("a /2 snapshot carries neither a capture nor a recovery")
  const coverage = r.coverage === undefined ? [] : r.coverage
  if (!Array.isArray(coverage) || coverage.length > SNAPSHOT_COVERAGE_MAX) bad("coverage is not a bounded list")
  return {
    schema,
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
    capture,
    recovery,
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
  const c = s.capture
  if (c) {
    if (!c.mapped) gaps.push("capture_unmapped")
    // No matched goroutine says only that the capture did not hold it, never that it was not stuck.
    if (c.matchedGoroutines === null || c.matchedGoroutines === "0") gaps.push("capture_not_matched")
    if (c.overBudget) gaps.push("capture_over_budget")
    if (c.rawTruncated || c.truncated) gaps.push("capture_truncated")
    if (c.sourceUnavailable === null || c.sourceUnavailable !== "0") gaps.push("capture_source_unavailable")
    if (c.redacted === null || c.redacted !== "0") gaps.push("capture_functions_redacted")
  }
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
      capture: snapshot.capture ? {
        seq: snapshot.capture.seq, duration_ns: snapshot.capture.durationNs, over_budget: snapshot.capture.overBudget,
        function: snapshot.capture.function, mapped: snapshot.capture.mapped, matched_goroutines: snapshot.capture.matchedGoroutines,
        sources: snapshot.capture.sources, state: snapshot.capture.state,
        frames: snapshot.capture.frames.map((f) => ({ function: f.function, source: f.source, source_kind: f.sourceKind })),
        frames_omitted: snapshot.capture.framesOmitted, goroutines_total: snapshot.capture.goroutinesTotal,
        raw_truncated: snapshot.capture.rawTruncated, truncated: snapshot.capture.truncated,
        functions_redacted: snapshot.capture.redacted, source_unavailable: snapshot.capture.sourceUnavailable,
      } : null,
      recovery: snapshot.recovery ? { state: snapshot.recovery.state, recovery_ns: snapshot.recovery.recoveryNs } : null,
    },
    evidence_gaps: evidenceGaps(snapshot),
  }, null, 2)
}

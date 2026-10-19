/** Keep failed reads eligible for recovery even before the first list arrives. */
export class ScheduleRecovery {
  private failed = new Set<string>()
  private ready = new Set<string>()
  private retried = new Set<string>()

  /** A regular refresh starts a new retry allowance; a recovery read does not. */
  begin(): void { this.retried.clear() }
  reset(): void { this.failed.clear(); this.ready.clear(); this.retried.clear() }
  answered(machine: string): void { this.failed.delete(machine) }
  missed(machine: string): void { this.failed.add(machine) }
  machineReady(machine: string): void { this.ready.add(machine) }
  takeRetry(): boolean {
    const missing = [...this.failed].filter((machine) => this.ready.has(machine) && !this.retried.has(machine))
    for (const machine of missing) this.retried.add(machine)
    return missing.length > 0
  }
}

/** Only an answered machine may replace its rows, including with an empty list. */
export function retainScheduleRows<Row extends { machine?: string }>(
  previous: readonly Row[], answers: ReadonlyMap<string, Row[]>,
): Row[] {
  return [...previous.filter((row) => !row.machine || !answers.has(row.machine)), ...[...answers.values()].flat()]
}

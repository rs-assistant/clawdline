/** The copied Cloud client's localStorage cache keeps only 64 command words. */
const SAVED_COMMANDS_CUT = 64

export interface CommandClient {
  readonly orchestratorSnapshots?: ReadonlyMap<string, unknown>
  machineDescriptor?(machine: string): { machine?: { commands?: unknown } } | null
}

/** Only a complete descriptor can prove that a machine lacks a command. */
export function machineCommands(client: CommandClient, machine: string): { commands: string[] | null; incomplete: boolean } {
  const snapshot = client.orchestratorSnapshots?.get(machine) as { machine?: { commands?: unknown } } | undefined
  if (snapshot?.machine && typeof snapshot.machine === "object" && !Array.isArray(snapshot.machine)) {
    const commands = snapshot.machine.commands
    return { commands: Array.isArray(commands) ? commands.filter((word): word is string => typeof word === "string") : null,
      incomplete: false }
  }
  const remembered = client.machineDescriptor?.(machine)?.machine?.commands
  if (!Array.isArray(remembered)) return { commands: null, incomplete: false }
  if (remembered.length >= SAVED_COMMANDS_CUT) return { commands: null, incomplete: true }
  return { commands: remembered.filter((word): word is string => typeof word === "string"), incomplete: false }
}

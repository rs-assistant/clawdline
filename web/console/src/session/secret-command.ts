/*
 * The receiving command a Secret Handoff Note names, as the person reads it
 * beside the field before handing off (docs/secret-handoff.md).
 *
 * The daemon gives the value only to this exact argv, so the line shown must
 * say plainly which program runs and with which arguments. Each argument is
 * written as a POSIX shell word: a plain one as it is, anything else in single
 * quotes, so a space, an empty argument or a quoted command line inside
 * `sh -c` cannot run two arguments together or hide one. The line is text: the
 * card renders it as a React text node, never as markup.
 */

const plainWord = /^[A-Za-z0-9_@%+=:,./-]+$/u

/** One argv entry as a shell word. */
function shellWord(arg: string): string {
  if (plainWord.test(arg)) return arg
  return `'${arg.replaceAll("'", `'\\''`)}'`
}

/**
 * The command line to show, or null when the Note names no usable command —
 * a Note written before commands were declared, or a malformed row. A Note
 * without one cannot be handed off: the daemon refuses its submission.
 */
export function secretHandoffDestination(command: unknown): string | null {
  if (!Array.isArray(command) || command.length === 0) return null
  if (!command.every((arg) => typeof arg === "string") || command[0] === "") return null
  return (command as string[]).map(shellWord).join(" ")
}

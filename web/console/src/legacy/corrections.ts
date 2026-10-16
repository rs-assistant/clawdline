/**
 * Deliberate corrections around the byte-locked Swift web copy.
 *
 * The copied files remain the evidence of what the former app shipped. Each
 * entry names the exact source line this console compensates for, why the
 * copied behaviour is false in this console, and the truthful replacement.
 * `refusals/scan.test.ts` keeps every source anchor live: once the locked copy
 * no longer contains it, the test fails and the obsolete correction must go.
 */
export interface LegacyCorrection {
  id:
    | "session-state-unrecognized"
    | "device-session-count-unknown"
    | "session-activity-not-drawn"
  sources: readonly {
    file: string
    line: number
    contains: string
  }[]
  copiedBehaviour: string
  whyWrong: string
  consoleBehaviour: string
}

export const LEGACY_CORRECTIONS: readonly LegacyCorrection[] = [
  {
    id: "session-state-unrecognized",
    sources: [
      {
        file: "web/console/src/legacy/js/core/i18n.js",
        line: 45,
        contains: 'webStateUnreadable: "screen could not be read"',
      },
      {
        file: "web/console/public/strings/zh-Hant.json",
        line: 749,
        contains: '"webStateUnreadable": "畫面讀不到"',
      },
    ],
    copiedBehaviour: "Says the screen could not be read when a session state is unknown.",
    whyWrong: "The screen bytes were read; ReadState could not identify what the visible screen meant.",
    consoleBehaviour: "Says the state could not be identified and directs the reader into the session to inspect it.",
  },
  {
    id: "device-session-count-unknown",
    sources: [
      {
        file: "web/console/src/legacy/js/view/devices.js",
        line: 46,
        contains: "sessions: Number.isSafeInteger(machine.sessions)",
      },
    ],
    copiedBehaviour: "Draws every supplied safe integer as a session count, and omits a non-integer.",
    whyWrong: "The copied Cloud client supplied 0 when this browser had no key with which to read the machine's sessions.",
    consoleBehaviour: "Supplies no integer without a readable count and adds whether the count is unreadable here or not known yet.",
  },
  {
    id: "session-activity-not-drawn",
    sources: [
      {
        file: "web/console/src/legacy/list.css",
        line: 68,
        contains: ".row .meta {",
      },
    ],
    copiedBehaviour: "Leaves the session row's last activity out of the metadata line.",
    whyWrong: "The daemon supplies that timestamp and the list already orders by it, so hiding it leaves the visible order unexplained.",
    consoleBehaviour: "Shows a known activity timestamp as relative time and draws nothing when the timestamp is absent or unknown.",
  },
] as const

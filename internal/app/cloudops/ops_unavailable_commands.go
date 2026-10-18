package cloudops

import ()

func init() {
	register(
		// MARK: commands this daemon refuses or has no local capability for

		op{name: "dispatch", refusal: &cloudDispatchUnpinned, anyClass: true,
			decode: func(b body) (plan, bool) {
				// Only enough to name the answer channel. The refusal is the
				// whole answer, and parsing a task this machine will not run
				// would be pretending otherwise. A dispatch that named no
				// request — which is what the hosted console sends today — is
				// still refused, as a notice with nowhere to publish rather
				// than as silence.
				if raw, named := b["session"]; named {
					if id, ok := raw.(string); !ok || id != MachineReplySession {
						return plan{}, false
					}
				}
				request, ok := requestName(b["request"])
				if !ok {
					return plan{}, true
				}
				return plan{session: MachineReplySession, request: request,
					name: "action:" + request}, true
			}},

		op{name: "diagnostics.report", readLevel: true},

		op{name: "diagnostics.events", readLevel: true},
	)
}

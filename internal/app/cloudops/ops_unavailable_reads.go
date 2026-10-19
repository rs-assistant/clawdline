package cloudops

import ()

func init() {
	register(
		// MARK: reads this daemon has no local capability for

		// The rows again, for a page that has just (re)connected.
		//
		// The relay replays each channel's last envelope from memory, that
		// memory dies with the account's object, and this machine re-sends an
		// unchanged row only on its heartbeat — so a page opened after an
		// eviction sees whichever rows happened to change, and the list is
		// short until the heartbeats come round. The copied client asks this
		// word of every machine that lists it in `features`, once per
		// connection that did not take over a live socket (`_recoverSessions`
		// in `net/cloud-client.js`); `orchestrator` is present, and true, only
		// when that page holds no `orch/` snapshot of this machine. The answer
		// is `{"sessions":[ids],"complete":bool}` after the rows went out.
		op{name: sessionsSnapshotWord, read: true, sessions: true},

		// A first recovery is a separate word so a console deployed before its
		// daemon never adds a field to sessions.snapshot that the older exact-key
		// decoder must refuse. Only a machine advertising this word is asked it.
		// The page sends it with the same `orchestrator: true` sessions.snapshot
		// carries when it holds no `orch/` snapshot (`_askSessionSnapshot` in
		// net/cloud-client.js builds `extra` the same way for both words), so
		// the exact three keys alone refused every first recovery of a page
		// that had not yet heard the task list.
		op{name: sessionsSnapshotInitialWord, read: true, sessions: true},

		// One Session's row, now (`Bridge.SessionRow`). `row` names it; the
		// envelope's own `session` is the machine's, as for the two words above.
		// Only a machine advertising this word is asked it.
		op{name: sessionsRowWord, read: true, sessions: true},

		// The token bill (docs/token-ledger.md, "What a person and a session
		// see"): one conversation, one child task, one Board item. Each is the
		// local route's own question with its id in the path, so each takes
		// exactly the ids that route takes and nothing else. The answer is the
		// route's, whole — a `reason` of `not_yet_read` or `transcript_missing`
		// rides inside a 200, and an unknown id is the route's own 404.
		op{name: "usage.session", read: true},
		op{name: "usage.overview", read: true},
		op{name: "usage.task", read: true},
		op{name: "usage.item", read: true},

		// Whether compacting early paid (docs/token-ledger.md "Did compacting
		// early pay"): the local route's own question, with its one query
		// field, `since`, taken exactly as the route takes it and nothing else.
		op{name: "usage.compare-compaction", read: true},

		// Whether handing over paid (docs/token-ledger.md "Did handing over
		// pay"): the same one query field, `since`, as the comparison above.
		op{name: "usage.compare-handoff", read: true},

		// What each unit of work added (docs/token-ledger.md "One unit of
		// work"): the same one query field, `since`, as the comparison.
		op{name: "usage.work-units", read: true},

		// The raw samples a before/after report is folded from
		// (docs/token-ledger.md "Did a change make one unit of work
		// cheaper"): the local route's own question, with its two query
		// fields, `since` as the comparison spells it and `until` a Unix
		// time, taken exactly as the route takes them and nothing else.
		op{name: "usage.work-samples", read: true},

		// Things waiting to be verified (docs/verifications.md). The phone is
		// where the person reads them, so every route crosses: two machine
		// reads, and five commands that carry `X-Clawdline-Actor: device` for
		// the reason the schedule writes do — a press on a phone is the person,
		// and a note it writes is signed as the person, not as this machine's
		// own hand. Each sub-document goes to the route whole; the route owns
		// its shape and refuses a field it does not read by name.
		op{name: "verification.list", read: true},

		op{name: "verification.get", read: true},

		op{name: "verification.create"},

		op{name: "verification.note"},

		op{name: "verification.close"},

		op{name: "verification.criterion"},

		// One record, by its id, and never more: the wire has no word that
		// deletes a list. `force` is the person saying an open one may go.
		op{name: "verification.delete"},

		// The sentences somebody wrote once, read from a phone.
		//
		// **The whole machine's list, and no session in the question.** The
		// producer sends `{type, session, request}` and nothing else
		// (`net/cloud-client.js`, `_freshSnippets`), because the rows ride the
		// `orch/<machine>` inventory rather than answering one session — so the
		// wire has nowhere to put a session and this read must not invent one.
		op{name: "snippets", read: true,
			divergence: "the whole machine's list, unfiltered: the local route takes `?session=` " +
				"and answers that session's two groups plus the project this machine resolved " +
				"for it — registry prefix first, then the checkout an isolated worktree was cut " +
				"from — and this wire carries no session, so the viewer groups the rows by " +
				"matching `project` against the session's own `cwd` exactly " +
				"(`view/snippets-data.js`, `snippetGroups`). A session standing in a " +
				"subdirectory of its project, or in a worktree, therefore sees its global " +
				"snippets and an empty project group where the same session on this machine's " +
				"own network sees both",
		},

		// Resolve the Project on the machine for a selected Session. The answer
		// carries the same Project identity and scoped rows as the local sheet.
		op{name: "session-snippets", read: true},

		op{name: "schedule", read: true},
	)
}

// SessionRowIDBytesLimit is the longest Session id a `sessions.row` read may
// name. A tmux pane id or an iTerm2 GUID is a few dozen bytes; anything past
// this is not an id this machine issued, and is refused before it is looked up.
const SessionRowIDBytesLimit = 256

package capacity

// connectionHealthBounds are the cloud.health_* rows: the connection-health
// evidence foundation (internal/domain/connectionhealth,
// docs/admin-connection-health-contract.md). They are named cloud.* and served
// by the cloud area, whose measure file reads them once the tracker is wired
// into the daemon; until then they are registered but unmeasured. Every limit
// is the plan's proposed starting bound, not a measured budget.
func connectionHealthBounds() []Entry {
	return []Entry{
		{
			// Every active-phase slot. A Begin that finds none free is
			// refused, counted by class and interval, and the operation runs
			// uninstrumented; the watchdog then reports trace coverage partial.
			Name: "cloud.health_active_slots", Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxActiveSlots"},
		},
		{
			// The slots only the reader, writer, drain, keepalive and
			// watchdog loops may take, so business work cannot exhaust the
			// phases that prove progress. Refused and counted as critical.
			Name: "cloud.health_reserved_slots", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxReservedSlots"},
		},
		{
			// The in-memory journal ring, by records and by bytes: the
			// oldest is evicted and counted, and a reader behind it is told
			// the exact sequence gap.
			Name: "cloud.health_journal_rows", Class: Journal, Unit: Rows,
			Limit: 4096, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxJournalRecords"},
		},
		{
			Name: "cloud.health_journal_bytes", Class: Journal, Unit: Bytes,
			Limit: 4 << 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxJournalBytes"},
		},
		{
			// The queue between a watchdog scan and the journal writer. A
			// scan never waits: a transition that finds it full is dropped
			// and counted, the report says transitions partial, and the
			// writer journals the count as transitions_lost where it happened.
			Name: "cloud.health_pending_transitions", Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{
				"internal/domain/connectionhealth.MaxPendingTransitions",
				"internal/domain/connectionhealth.NewWatchdog:chan(MaxPendingTransitions)",
			},
		},
		{
			// One encoded journal record; a larger one is refused and counted.
			Name: "cloud.health_record_bytes", Class: Buffer, Unit: Bytes,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxRecordBytes"},
		},
		{
			// One journal query page, by records and by bytes. The page
			// stops, says truncated and carries the cursor to continue.
			Name: "cloud.health_query_rows", Class: Observation, Unit: Rows,
			Limit: 200, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxQueryRecords"},
		},
		{
			Name: "cloud.health_query_bytes", Class: Observation, Unit: Bytes,
			Limit: 1 << 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxQueryBytes"},
		},
		{
			// The capture worker's one raw goroutine dump buffer. A dump that
			// fills it is cut by the runtime; the capture says raw_truncated.
			// It never leaves the worker: not exported, not persisted.
			Name: "cloud.health_capture_raw_bytes", Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxCaptureRawBytes"},
		},
		{
			// One sanitized capture as JSON: goroutines are dropped from the
			// end, the stalled one last, and the capture says truncated.
			Name: "cloud.health_capture_export_bytes", Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxCaptureExportBytes"},
		},
		{
			// Frames kept per captured goroutine; the rest are counted.
			Name: "cloud.health_capture_frames", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxCaptureFrames"},
		},
		{
			// Captures that may start in one minute. A trigger past it is
			// refused and counted as rate limited; the stall record stands.
			Name: "cloud.health_captures_per_minute", Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxCapturesPerWindow"},
		},
		{
			// The proposed completion budget. runtime.Stack cannot be
			// cancelled: past it the worker reads timed_out_unavailable,
			// triggers are refused and counted, and no second worker starts.
			Name: "cloud.health_capture_budget_seconds", Class: Buffer, Unit: Seconds,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/connectionhealth.MaxCaptureBudgetSeconds"},
		},
		{
			// Stalls the owner-sealed feed remembers so a capture or a
			// recovery can revise the stall's attachment. The oldest is
			// forgotten: its recovery is then not sealed (the local export
			// still has it).
			Name: "cloud.health_owner_incidents", Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.maxOwnerIncidents"},
		},
	}
}

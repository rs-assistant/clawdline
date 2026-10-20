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
	}
}

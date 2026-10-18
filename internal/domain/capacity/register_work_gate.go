package capacity

// workGateBounds is every registered row named work_gate_*.
func workGateBounds() []Entry {
	return []Entry{
		{
			// Work v2 owns the detailed rounds retained for one item. At the
			// limit a new round is refused as verification_rounds_full until a
			// person exports eligible closed detail and confirms its digest-
			// bound purge. No detail is silently evicted.
			Name: WorkGateRoundDetailsPerItem, Class: Evidence, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Notice, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/contract.WorkGateRoundDetailsPerItemLimit"},
		},
		{
			// The Work v2 store owns the global retained-round population. It
			// has the same explicit export-and-confirmed-purge recovery as the
			// per-item row and never removes the oldest row automatically.
			Name: WorkGateRoundDetailsPerStore, Class: Evidence, Unit: Rows,
			Limit: 10_000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Notice, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/contract.WorkGateRoundDetailsPerStoreLimit"},
		},
		{
			// The coordinator and broker own one initial gate task and one
			// bounded retry. A third protocol-lineage record is refused and the
			// round becomes a technical escalation.
			Name: WorkGateTasksPerRound, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateTasksPerRoundLimit"},
		},
		{
			// The broker owns checker-result admission. An oversized claim set
			// is a typed technical failure and no partial result is retained.
			Name: WorkGateClaimsPerRound, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateClaimsPerRoundLimit"},
		},
		{
			Name: WorkGateEvidenceStringsPerClaim, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceStringsPerClaimLimit"},
		},
		{
			Name: WorkGateEvidenceStringBytes, Class: Buffer, Unit: Bytes,
			Limit: 500, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceStringBytesLimit"},
		},
		{
			// Gate-result HTTP admission rejects a body before persistence once
			// it exceeds this byte ceiling.
			Name: WorkGateResultBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateResultBytesLimit"},
		},
		{
			// The broker owns streamed artifact admission. Count and byte
			// ceilings reject the incoming upload before it changes durable
			// task evidence; eligible round export/purge is separate recovery.
			Name: WorkGateEvidenceArtifactsPerTask, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceArtifactsPerTaskLimit"},
		},
		{
			Name: WorkGateEvidenceArtifactBytes, Class: Buffer, Unit: Bytes,
			Limit: 2 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceArtifactBytesLimit"},
		},
		{
			Name: WorkGateEvidenceTotalBytesPerTask, Class: Buffer, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceTotalBytesPerTaskLimit"},
		},
		{
			// A single-item Work v2 read owns this newest-first observation.
			// Older detail stays in the store and aggregates/latest remain in
			// the answer, which says the detail list was truncated.
			Name: WorkGateRecentRoundsPerItemRead, Class: Observation, Unit: Rows,
			Limit: 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateRecentRoundsPerItemReadLimit"},
		},
		{
			// The coordinator owns this due-row observation. Rows beyond one
			// pass remain queued and the next supervised pass resumes from a
			// stable cursor; none is removed from durable work.
			Name: WorkGateDueRowsPerPass, Class: Observation, Unit: Rows,
			Limit: 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateDueRowsPerPassLimit"},
		},
		{
			// The coordinator owns provider/capacity retry timing. At this age
			// the delay expires and the row becomes due; a provider's larger
			// requested delay is capped here rather than extending forever.
			Name: WorkGateRetryBackoffSeconds, Class: Cache, Unit: Seconds,
			Limit: 300, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateRetryBackoffSecondsLimit"},
		},
		{
			// The coordinator owns the live-parent observation. Once this
			// observation is stale, authority is atomically promoted to the
			// person and one attention notification is owed.
			Name: WorkGateOwnerOfflineGraceSeconds, Class: Observation, Unit: Seconds,
			Limit: 900, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Notice}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateOwnerOfflineGraceSecondsLimit"},
		},
	}
}

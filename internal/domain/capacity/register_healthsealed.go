package capacity

// healthSealedBounds are the cloud.health_sealed_* rows: the machine side of
// Cloud's owner-sealed diagnostic attachments (internal/adapters/healthsealed,
// Cloud's docs/connection-health-contract.md §13). The ciphertext, request,
// revision and life rows mirror Cloud's SEALED_LIMITS so the machine stops
// itself before Cloud refuses it; the store, queue and attempt rows are this
// producer's proposals, not measured budgets.
func healthSealedBounds() []Entry {
	return []Entry{
		{
			// ciphertext||tag of one revision; a snapshot that would seal
			// larger is refused (too_large), never truncated.
			Name: "cloud.health_sealed_ciphertext_bytes", Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxCiphertextBytes"},
		},
		{
			Name: "cloud.health_sealed_request_bytes", Class: Buffer, Unit: Bytes,
			Limit: 400 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxRequestBytes"},
		},
		{
			// Immutable revisions of one attachment; the next is refused
			// (revisions_exhausted).
			Name: "cloud.health_sealed_revisions", Class: Idempotency, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxRevisions"},
		},
		{
			// An attachment's life from its first revision; past it the
			// local record is pruned and Cloud answers 410.
			Name: "cloud.health_sealed_life_seconds", Class: Idempotency, Unit: Seconds,
			Limit: 7 * 24 * 60 * 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxLifeSeconds"},
		},
		{
			Name: "cloud.health_sealed_snapshot_bytes", Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotBytes"},
		},
		{
			Name: "cloud.health_sealed_snapshot_field_chars", Class: Buffer, Unit: Characters,
			Limit: 512, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotFieldChars"},
		},
		{
			Name: "cloud.health_sealed_snapshot_coverage", Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotCoverage"},
		},
		{
			// Local attachment records (each holds its revisions' exact
			// bodies). A new attachment at the bound is refused at once
			// (store_full); expired records are pruned.
			Name: "cloud.health_sealed_store_attachments", Class: Idempotency, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxPendingAttachments"},
		},
		{
			Name: "cloud.health_sealed_store_bytes", Class: Idempotency, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxStoreBytes"},
		},
		{
			// Sends of one revision per Send call; past it the revision
			// stays pending with the same bytes.
			Name: "cloud.health_sealed_attempts", Class: Idempotency, Unit: Rows,
			Limit: 4, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxAttempts"},
		},
		{
			Name: "cloud.health_sealed_attempt_seconds", Class: Buffer, Unit: Seconds,
			Limit: 10, AtLimit: Disconnect,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxAttemptSeconds"},
		},
		{
			Name: "cloud.health_sealed_response_bytes", Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxResponseBytes"},
		},
		{
			// The worker's hand-off queue; Offer at the bound refuses at
			// once (busy) and counts the job as dropped.
			Name: "cloud.health_sealed_queued_jobs", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxQueuedJobs", "internal/adapters/healthsealed.NewWorker:chan(MaxQueuedJobs)"},
		},
	}
}

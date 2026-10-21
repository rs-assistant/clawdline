package capacity

// Settings → Remote's owner-diagnostics block
// (web/console/src/cloud/health-owner/retention.ts OWNER_RETENTION_LIMITS,
// used by OwnerRetentionBlock.tsx). Both live in each open Settings window;
// TestOwnerRetentionBoundsMatchTheBlock holds the two sides together.
const (
	ConsoleOwnerRetentionPollSeconds    = "console.owner_retention_poll_seconds"
	ConsoleOwnerRetentionRequestSeconds = "console.owner_retention_request_seconds"
)

// ownerRetentionBounds is every console.owner_retention_* row.
func ownerRetentionBounds() []Entry {
	return []Entry{
		{
			// A shown block reads the switch and the sender's status again at
			// this pace (pollMs). A tick that finds the last read still out is
			// skipped, never queued: one refresh at a time.
			Name: ConsoleOwnerRetentionPollSeconds, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One refresh, both local reads together (requestMs). Past it both
			// are aborted and whatever had not answered reads "unknown
			// (timeout)", never the last value as if it were current.
			Name: ConsoleOwnerRetentionRequestSeconds, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
	}
}

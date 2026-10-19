package capacity

func usageOverviewBounds() []Entry {
	return []Entry{
		{Name: "usage.daily_days", Class: Journal, Unit: Rows, Limit: 57, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/transcript.DailyDayLimit"}},
		{Name: "usage.daily_rows", Class: Journal, Unit: Rows, Limit: 50000, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/store.UsageDailyRowLimit"}},
		{Name: "usage.overview_tasks", Class: Observation, Unit: Rows, Limit: 10000, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/store.UsageOverviewTaskLimit"}},
	}
}

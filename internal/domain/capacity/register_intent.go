package capacity

// intentBounds is every registered row named intent.*.
func intentBounds() []Entry {
	return []Entry{
		{
			// One spoken sentence admitted by /v1/intents. A larger body is
			// refused before a model is asked to read it.
			Name: IntentRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.intentLimit",
				"internal/app/cloudops.intentTextLimit"},
		},
		{
			// One planner turn running and one waiting. A third is refused
			// with 429 busy and can try again after the line moves.
			Name: IntentPlannerQueue, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.intentQueueLimit"},
		},
		{
			// A CLI turn past this deadline is stopped and gives its queue
			// slot back. The sender receives plan_failed.
			Name: IntentPlannerSeconds, Class: Buffer, Unit: Seconds,
			Limit: 30, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.intentTimeLimit"},
		},
		{
			// The hosted console may be the one admitted waiter: 60 seconds
			// behind the active turn, then 60 for its own two CLI attempts,
			// with ten seconds for relay delivery and refusal handling.
			Name: IntentCloudWaitSeconds, Class: Buffer, Unit: Seconds,
			Limit: 130, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.intentCloudWaitLimit"},
		},
		{
			// The end of a failed planner or naming turn's stderr, kept so a
			// CLI's usage-limit refusal can be told apart from any other
			// failure. Older bytes give way to newer ones.
			Name: IntentStderrBytes, Class: Observation, Unit: Bytes,
			Limit: 4 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/planner.stderrLimit"},
		},
	}
}

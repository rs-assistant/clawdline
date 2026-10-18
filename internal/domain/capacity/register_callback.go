package capacity

// callbackBounds is every registered row named callback.*.
func callbackBounds() []Entry {
	return []Entry{
		{
			// A callback naming more words, or more bytes of them, is refused
			// bad_task and nothing is started.
			Name: CallbackArgs, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxCallbackArgs"},
		},
		{
			Name: CallbackCommandBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.CallbackCommandLimit"},
		},
		{
			// Callbacks still running, per root and on this machine. They do
			// not take a child's slot; one past either cap is refused
			// callback_capacity and nothing is started.
			Name: CallbacksPerRoot, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxCallbacksPerRoot"},
		},
		{
			Name: CallbacksPerMachine, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxCallbacksPerMachine"},
		},
		{
			// A running command's output.log. Past the limit the beat keeps
			// its newest output and says on the first line that earlier
			// output was dropped.
			Name: CallbackOutputBytes, Class: Observation, Unit: Bytes,
			Limit: 8 << 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.CallbackLogLimit"},
		},
		{
			// The end of output.log read for a finished callback's verdict;
			// earlier output stays in the file for `task show`'s reader.
			Name: CallbackTailBytes, Class: Observation, Unit: Bytes,
			Limit: 8 << 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.CallbackTailLimit"},
		},
	}
}

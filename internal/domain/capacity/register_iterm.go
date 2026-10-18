package capacity

// itermBounds is every registered row named iterm.*.
func itermBounds() []Entry {
	return []Entry{
		{
			// The newest failed iTerm2 osascript runs, held for the next
			// stall diagnosis (internal/adapters/terminal/stall.go). Past the
			// limit the oldest is let go; every one of them is already a line
			// in the log.
			Name: ITermStallFailures, Class: Observation, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallFailuresLimit"},
		},
		{
			// The osascript run kinds counted apart (osascript_stats.go):
			// the fixed script names in the terminal adapter. A kind past
			// the limit is counted as `other`, so the counts never grow
			// and never lose a run.
			Name: ITermOsascriptKinds, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Coalesce,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.osascriptKindsLimit"},
		},
		{
			// An optional iTerm2 viewer runs after the tmux pane was returned.
			// Its timeout cannot hold the browser's Session start response.
			Name: ITermViewerOpenSeconds, Class: Buffer, Unit: Seconds,
			Limit: 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/tmuxview.viewerOpenLimit"},
		},
		{
			// A stalled Apple Event may occupy the viewer lane, but further
			// starts still return their tmux pane without queuing viewers.
			Name: ITermViewerConcurrent, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/tmuxview.viewerSlotLimit", "internal/app/tmuxview.package:chan(viewerSlotLimit)"},
		},
		{
			// The end of what osascript wrote on stderr that one failure line
			// carries. The end is kept: the Apple Event error number is there.
			Name: ITermStallSaidBytes, Class: Observation, Unit: Bytes,
			Limit: 512, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.saidLimit"},
		},
		{
			// The stall diagnoses kept in CLAWDLINE_NEXT_DIR/logs. After each
			// one is written the oldest past the limit are removed.
			Name: ITermStallDiagnoses, Class: DiagnosticLog, Unit: Rows,
			Limit: 20, AtLimit: Rotate,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallDiagnosesLimit"},
		},
		{
			// The least time between two stall diagnoses: one per episode,
			// which has run 3 to 23 minutes. A run of timeouts inside it
			// starts nothing; its failures are still logged one by one.
			Name: ITermStallCooldown, Class: Buffer, Unit: Seconds,
			Limit: 1800, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallCooldownLimit"},
		},
		{
			// The most of one step's output a stall diagnosis carries. A
			// two-second sample of iTerm2 measured 245 KB here; past the
			// limit the rest is not written and the section says how much.
			Name: ITermStallSectionBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallSectionLimit"},
		},
		{
			// The longest one step of a stall diagnosis may take. A step past
			// it is abandoned and recorded as timed out; the file goes on.
			Name: ITermStallStepSeconds, Class: Buffer, Unit: Seconds,
			Limit: 30, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallStepLimit"},
		},
	}
}

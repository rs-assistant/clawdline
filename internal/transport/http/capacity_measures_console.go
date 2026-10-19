package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// consoleCapacityMeasures measures every row of the register's "console" area
// (capacity.AreaOf), one reading per row.
func (s *Server) consoleCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		// The console's transcript cadence lives in each open tab; the
		// daemon serves the reads and cannot count a tab's timers.
		capacity.ConsoleTranscriptSafetySeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFollowFast: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFollowLater: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleHealthLive: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleSessionNotesSafety: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFollowSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptMergedRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab guard; daemon cannot measure a tab's merged page"}
		},
		capacity.ConsoleTranscriptBackoffSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser retries"}
		},
		capacity.ConsoleRelayTranscriptExpectReask: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console relay reader cadence; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleRelayFeatureWait: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console relay reader wait for this machine's own word list; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleStatusGapRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retry of a missing Cloud status row; daemon cannot measure another device's reads"}
		},
		capacity.ConsolePersonaRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retry of a transient role catalog failure; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleStatusMarkerRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retry of a missing Cloud status inventory; daemon cannot measure another device's reads"}
		},
		capacity.ConsolePinnedListReadTimeout: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console pinned machine list read deadline; daemon cannot measure another device's wait"}
		},
		capacity.ConsoleStatusMarkerRecoveryWait: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retained inventory replay deadline; daemon cannot measure another device's wait"}
		},
		capacity.ConsoleOpenedRowRestate: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console relay reader cadence for the opened Session's own row; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleOpenedRowQuiet: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab wait before the pane says the row has not come; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleDirectHistoryHours: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser direct-attempt record in that browser's storage; daemon cannot read another device's storage"}
		},
		capacity.ConsoleDirectHistoryMachines: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser direct-attempt record in that browser's storage; daemon cannot read another device's storage"}
		},
		capacity.ConsoleMachineListPoll: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
	}
}

package http

import (
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/tmuxview"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// itermCapacityMeasures measures every row of the register's "iterm" area
// (capacity.AreaOf), one reading per row.
func (s *Server) itermCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		// The iTerm2 stall watch (internal/adapters/terminal/stall.go): the
		// failures it holds, and the diagnosis files in the logs directory.
		capacity.ITermStallFailures: func() capacity.Reading {
			failures, _, _ := terminal.ITermStallReading()
			return capacity.Reading{Known: true, Used: int64(failures)}
		},
		capacity.ITermStallDiagnoses: func() capacity.Reading {
			_, diagnoses, err := terminal.ITermStallReading()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(diagnoses)}
		},
		capacity.ITermOsascriptKinds: func() capacity.Reading {
			total, _ := terminal.OsascriptReading()
			return capacity.Reading{Known: true, Used: int64(len(total.Kinds))}
		},
		capacity.ITermViewerOpenSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "background viewer deadline; no start waits for it"}
		},
		capacity.ITermViewerConcurrent: func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(tmuxview.ViewerOpenCount())}
		},
		capacity.ITermStallSaidBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-failure tail of osascript's stderr; held with the failure"}
		},
		capacity.ITermStallCooldown: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "least time between two diagnoses; no retained wait"}
		},
		capacity.ITermStallSectionBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-step output cap; released when the diagnosis is written"}
		},
		capacity.ITermStallStepSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-step deadline; no retained wait"}
		},
	}
}

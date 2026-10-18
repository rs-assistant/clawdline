package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// intentCapacityMeasures measures every row of the register's "intent" area
// (capacity.AreaOf), one reading per row.
func (s *Server) intentCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.IntentRequestBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.IntentPlannerQueue: func() capacity.Reading {
			s.intentMu.Lock()
			defer s.intentMu.Unlock()
			return capacity.Reading{Known: true, Used: int64(s.intentQueued)}
		},
		capacity.IntentPlannerSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-turn deadline; no retained buffer"}
		},
		capacity.IntentCloudWaitSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "hosted-console request deadline; no retained buffer"}
		},
		capacity.IntentStderrBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-turn tail of a failed CLI's stderr; released with the turn"}
		},
	}
}

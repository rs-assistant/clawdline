package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// callbackCapacityMeasures measures every row of the register's "callback" area
// (capacity.AreaOf), one reading per row.
func (s *Server) callbackCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.CallbackArgs:         func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-callback guard"} },
		capacity.CallbackCommandBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-callback guard"} },
		capacity.CallbacksPerRoot:     func() capacity.Reading { return capacity.Reading{Known: true, Note: "checked at each start"} },
		capacity.CallbacksPerMachine:  func() capacity.Reading { return capacity.Reading{Known: true, Note: "checked at each start"} },
		capacity.CallbackOutputBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-callback log, kept by the beat"}
		},
		capacity.CallbackTailBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-verdict read"} },
	}
}

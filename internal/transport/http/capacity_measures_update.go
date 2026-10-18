package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// updateCapacityMeasures measures every row of the register's "update" area
// (capacity.AreaOf), one reading per row.
func (s *Server) updateCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.UpdateRefreshSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "refresh period of the update check; one answer is held, replaced only by a good new one"}
		},
		capacity.UpdateFetchTimeoutSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read timing ceiling for the cloud's BUILD.json; no retained buffer"}
		},
		capacity.UpdateBuildBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard on one BUILD.json body; no retained buffer"}
		},
		capacity.UpdateApplyBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a per-read guard of the release updater; no retained buffer"}
		},
	}
}

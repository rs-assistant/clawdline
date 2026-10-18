package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// squadPackageCapacityMeasures measures every row of the register's "squadpackage" area
// (capacity.AreaOf), one reading per row.
func (s *Server) squadPackageCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.SquadPackageArchiveBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-ZIP guard"} },
		capacity.SquadPackageRequestBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request base64 envelope guard"}
		},
		capacity.SquadPackageManifestBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-manifest guard"} },
		capacity.SquadPackageFileBytes:     func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-entry guard"} },
		capacity.SquadPackageExpandedBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-ZIP expanded bytes guard"} },
		capacity.SquadPackageEntries:       func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-ZIP entry count guard"} },
		capacity.SquadPackageExpansionRatio: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-entry compression ratio guard"}
		},
		capacity.SquadPackagePreviewRows: func() capacity.Reading {
			n, err := s.store.SquadPackagePreviewUsage(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n}
		},
		capacity.SquadPackagePreviewAge: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "durable preview grants expire after 15 minutes"}
		},
	}
}

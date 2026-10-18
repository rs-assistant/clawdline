package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// projectSyncCapacityMeasures measures every row of the register's "projectsync" area
// (capacity.AreaOf), one reading per row.
func (s *Server) projectSyncCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"projectsync.mirrored": func() capacity.Reading {
			if s.projectSync == nil || s.projectSync.Mirror == nil {
				return capacity.Reading{Known: true, Note: "no project mirror attached"}
			}
			return capacity.Reading{Known: true, Used: s.projectSync.Mirror.Count()}
		},
		"projectsync.clones": func() capacity.Reading {
			if s.projectSync == nil {
				return capacity.Reading{Known: true, Note: "no project mirror attached"}
			}
			return capacity.Reading{Known: true, Used: int64(len(s.projectSync.State().Clones))}
		},
		"projectsync.manifest_projects": func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-manifest guard"} },
		"projectsync.project_files":     func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-project guard"} },
		"projectsync.file_bytes":        func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-file guard"} },
		"projectsync.path_bytes":        func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-path guard"} },
		"projectsync.entry_bytes":       func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-request byte guard"} },
	}
}

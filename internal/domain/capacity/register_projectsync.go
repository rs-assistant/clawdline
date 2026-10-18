package capacity

// projectSyncBounds is every registered row named projectsync.*.
func projectSyncBounds() []Entry {
	return []Entry{
		{
			Name: "projectsync.manifest_projects", Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxManifestProjects"},
		},
		{
			Name: "projectsync.project_files", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxProjectFiles"},
		},
		{
			Name: "projectsync.file_bytes", Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxFileBytes"},
		},
		{
			Name: "projectsync.path_bytes", Class: Buffer, Unit: Bytes,
			Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxPathBytes"},
		},
		{
			Name: "projectsync.entry_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxEntryBytes", "internal/app/cloudops.projectSyncCloudBodyLimit"},
		},
		{
			Name: "projectsync.mirrored", Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/domain/projectsync.MaxMirrorRecords"},
		},
		{
			Name: "projectsync.clones", Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectsync.MaxCloneJobs"},
		},
	}
}

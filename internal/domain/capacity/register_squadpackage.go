package capacity

// squadPackageBounds is every registered row named squadpackage.*.
func squadPackageBounds() []Entry {
	return []Entry{
		{
			Name: SquadPackageArchiveBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxArchiveBytes"},
		},
		{
			Name: SquadPackageRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxRequestBytes"},
		},
		{
			Name: SquadPackageManifestBytes, Class: Buffer, Unit: Bytes,
			Limit: 128 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxManifestBytes"},
		},
		{
			Name: SquadPackageFileBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxFileBytes"},
		},
		{
			Name: SquadPackageExpandedBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxExpandedBytes"},
		},
		{
			Name: SquadPackageEntries, Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxEntries"},
		},
		{
			Name: SquadPackageExpansionRatio, Class: Buffer, Unit: Multipliers,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxExpansionRatio"},
		},
		{
			Name: SquadPackagePreviewRows, Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/squadpackages.MaxPreviewRows"},
		},
		{
			Name: SquadPackagePreviewAge, Class: Cache, Unit: Seconds,
			Limit: 15 * 60, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/squadpackages.MaxPreviewAgeSeconds"},
		},
	}
}

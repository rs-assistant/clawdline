package capacity

// updateBounds is every registered row named update.*.
func updateBounds() []Entry {
	return []Entry{
		{
			// The cloud's BUILD.json, as the update check last read it. At
			// this age the background loop reads it again; the old answer is
			// replaced only by a good new one (docs/updates.md).
			Name: UpdateRefreshSeconds, Class: Observation, Unit: Seconds,
			Limit: 1800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/updatecheck.RefreshSecondsLimit"},
		},
		{
			// One ask of the cloud's BUILD.json. At the limit the ask is
			// abandoned and its error is shown beside the last good answer.
			Name: UpdateFetchTimeoutSeconds, Class: Observation, Unit: Seconds,
			Limit: 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/updatecheck.FetchTimeoutSecondsLimit"},
		},
		{
			// One BUILD.json body, from the cloud or the served dist. A
			// longer one is refused as not a BUILD.json.
			Name: UpdateBuildBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/updatecheck.maxBuildBodyBytes"},
		},
		{
			// One POST /v1/update/apply body. A longer one is refused as
			// bad_request.
			Name: UpdateApplyBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.updateApplyBodyLimit"},
		},
	}
}

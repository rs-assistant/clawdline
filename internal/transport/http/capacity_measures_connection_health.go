package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

// connectionHealthCapacityMeasures reads the cloud.health_* rows. The
// connection-health tracker, watchdog and journal (internal/domain/
// connectionhealth) are not constructed by this daemon yet, so each row holds
// nothing: a known zero that says why, not an unmeasured row. Wiring the
// tracker replaces these with its Snapshot and journal coverage.
func connectionHealthCapacityMeasures() map[string]func() capacity.Reading {
	const note = "connection-health tracker not wired into this daemon yet; nothing is held"
	out := map[string]func() capacity.Reading{}
	for _, name := range []string{
		"cloud.health_active_slots", "cloud.health_reserved_slots",
		"cloud.health_journal_rows", "cloud.health_journal_bytes", "cloud.health_record_bytes",
		"cloud.health_query_rows", "cloud.health_query_bytes",
	} {
		out[name] = func() capacity.Reading { return capacity.Reading{Known: true, Note: note} }
	}
	return out
}

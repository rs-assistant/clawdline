package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// connectionHealthCapacityMeasures reads the cloud.health_* rows from the
// daemon's connection-health runtime: the tracker's snapshot for the slot
// rows, the journal's occupancy and lifetime eviction and refusal counts,
// and the watchdog's transition queue and its drops. Each read is atomics or
// the journal's own short lock; none calls Scan.
//
// With monitoring off there is no runtime and each row is unmeasured with
// that reason, not a zero that would read as an empty table.
func (s *Server) connectionHealthCapacityMeasures() map[string]func() capacity.Reading {
	rt := s.connectionHealth
	names := []string{
		"cloud.health_active_slots", "cloud.health_reserved_slots",
		"cloud.health_journal_rows", "cloud.health_journal_bytes", "cloud.health_record_bytes",
		"cloud.health_pending_transitions",
		"cloud.health_query_rows", "cloud.health_query_bytes",
	}
	out := map[string]func() capacity.Reading{}
	if rt == nil {
		for _, name := range names {
			out[name] = func() capacity.Reading {
				return capacity.Unmeasured("connection-health monitoring is off in this daemon")
			}
		}
		return out
	}
	slots := func(critical bool) capacity.Reading {
		phases, overflow := rt.Tracker.Snapshot()
		var used int64
		for _, p := range phases {
			if p.Spec.Critical == critical || !critical {
				used++
			}
		}
		refused := overflow.Total
		if critical {
			refused = overflow.Critical
		}
		return capacity.Reading{Known: true, Used: used, Counters: capacity.Counters{Refused: int64(refused)}}
	}
	journal := func(pick func(connectionhealth.JournalStats) int64) func() capacity.Reading {
		return func() capacity.Reading {
			st := rt.Journal.Stats()
			return capacity.Reading{Known: true, Used: pick(st),
				Note:     connectionhealth.DurabilityMemory,
				Counters: capacity.Counters{Evicted: int64(st.Evicted), Refused: int64(st.Refused)}}
		}
	}
	out["cloud.health_active_slots"] = func() capacity.Reading { return slots(false) }
	out["cloud.health_reserved_slots"] = func() capacity.Reading { return slots(true) }
	out["cloud.health_journal_rows"] = journal(func(st connectionhealth.JournalStats) int64 { return int64(st.Rows) })
	out["cloud.health_journal_bytes"] = journal(func(st connectionhealth.JournalStats) int64 { return int64(st.Bytes) })
	out["cloud.health_record_bytes"] = journal(func(st connectionhealth.JournalStats) int64 { return int64(st.LargestRecord) })
	out["cloud.health_pending_transitions"] = func() capacity.Reading {
		depth, lost := rt.Watchdog.Pending()
		return capacity.Reading{Known: true, Used: int64(depth), Counters: capacity.Counters{Dropped: int64(lost)}}
	}
	// A page is built and answered within one request; nothing is held
	// between reads, so the per-page rows are a true zero.
	for _, name := range []string{"cloud.health_query_rows", "cloud.health_query_bytes"} {
		out[name] = func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a bound per page; nothing is held between reads"}
		}
	}
	return out
}

package http

import (
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// connectionHealthCapacityMeasures reads the cloud.health_* rows from the
// daemon's connection-health runtime: the tracker's snapshot for the slot
// rows, the journal's occupancy and lifetime eviction and refusal counts,
// and the watchdog's transition queue and its drops. Each read is atomics or
// the journal's own short lock; none calls Scan.
//
// With monitoring off there is no runtime, so nothing is held: each row is a
// known zero that says why, which the note keeps apart from an idle table.
func (s *Server) connectionHealthCapacityMeasures() map[string]func() capacity.Reading {
	rt := s.connectionHealth
	names := []string{
		"cloud.health_active_slots", "cloud.health_reserved_slots",
		"cloud.health_journal_rows", "cloud.health_journal_bytes", "cloud.health_record_bytes",
		"cloud.health_pending_transitions",
		"cloud.health_query_rows", "cloud.health_query_bytes",
	}
	out := s.connectionHealthStoreMeasures(rt != nil)
	if rt == nil {
		for _, name := range names {
			out[name] = func() capacity.Reading {
				return capacity.Reading{Known: true, Note: "connection-health monitoring is off in this daemon; nothing is held"}
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

// connectionHealthStoreMeasures reads the cloud.health_store_* rows from the
// durable store's Status (atomics and a copy of its segment list). Before the
// store is open, and with monitoring off, it holds nothing: a known zero
// that says which. A store that failed to open leaves what is on disk
// unknown, so its rows are unmeasured with the failure code. A per-item
// bound (a frame, a segment, the lost-range memory) that Status does not
// expose is unmeasured, not zero.
func (s *Server) connectionHealthStoreMeasures(monitoring bool) map[string]func() capacity.Reading {
	names := []string{
		"cloud.health_store_bytes", "cloud.health_store_seconds", "cloud.health_store_segment_bytes",
		"cloud.health_store_segments", "cloud.health_store_queue_rows", "cloud.health_store_queue_bytes",
		"cloud.health_store_frame_bytes", "cloud.health_store_readers", "cloud.health_store_dir_entries",
		"cloud.health_store_lost_ranges",
	}
	out := map[string]func() capacity.Reading{}
	for _, name := range names {
		name := name
		out[name] = func() capacity.Reading {
			d := s.connectionHealthStore.Load()
			switch {
			case !monitoring:
				return capacity.Reading{Known: true, Note: "connection-health monitoring is off in this daemon; nothing is held"}
			case d == nil || d.state == DurableOpening:
				return capacity.Reading{Known: true, Note: "the durable connection-health store is not open yet; nothing is held by it"}
			case d.store == nil:
				return capacity.Unmeasured("the durable connection-health store could not be opened: " + d.code)
			}
			return storeReading(name, d.store.Status(), time.Now())
		}
	}
	return out
}

func storeReading(name string, st healthjournal.Status, now time.Time) capacity.Reading {
	pruned := int64(st.PrunedAge + st.PrunedBytes + st.PrunedCount)
	r := capacity.Reading{Known: true, Failing: st.State != "ok" && st.State != "closed",
		Counters: capacity.Counters{Evicted: pruned, Dropped: int64(st.Lost)}}
	switch name {
	case "cloud.health_store_bytes":
		r.Used = st.Bytes
		r.Note = st.CrashLoss
	case "cloud.health_store_seconds":
		if st.Floor == nil {
			r.Note = "nothing is retained yet"
			break
		}
		r.Used = int64(now.Sub(*st.Floor) / time.Second)
		r.OldestAt = *st.Floor
	case "cloud.health_store_segments":
		r.Used = int64(st.Segments)
	case "cloud.health_store_queue_rows":
		r.Used = st.QueueRows
		r.Counters.Refused = int64(st.Refused)
	case "cloud.health_store_queue_bytes":
		r.Used = st.QueueBytes
		r.Counters.Refused = int64(st.Refused)
	case "cloud.health_store_readers":
		r.Used = int64(st.Readers)
	case "cloud.health_store_frame_bytes":
		r.Note = "a bound per frame; nothing is held between writes"
		r.Counters.Refused = int64(st.TooLarge)
	default:
		return capacity.Unmeasured("the durable store's status does not report this row's occupancy")
	}
	return r
}

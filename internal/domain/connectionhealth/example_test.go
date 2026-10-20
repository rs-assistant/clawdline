package connectionhealth

import (
	"encoding/json"
	"fmt"
	"time"
)

// The v1 evidence export for a read handler that never returns, exactly as
// docs/admin-connection-health-contract.md shows it.
func ExampleExport() {
	c := newFakeClock()
	tr := NewTracker(c)
	j, _ := NewJournal(c, "boot-7f3a", "c9fe745db066")
	w := NewWatchdog(tr, j, c, time.Second)
	reader, _ := tr.Begin(Spec{Source: SourceDaemon, Stage: StageIdle, Class: ClassTransport, Kind: LongLived, Location: "cloud.transport.read_loop", Critical: true})
	_ = reader
	h, _ := tr.Begin(Spec{Source: SourceDaemon, Stage: StageLaneQueued, Class: ClassRead, Kind: Finite,
		Location: "cloud.link.read_handler", Correlation: "tr-01HZX3", Warn: 5 * time.Second, Block: 15 * time.Second})
	c.advance(40 * time.Millisecond)
	_ = h.Progress(StageHandlerStarted)
	var r Report
	for i := 0; i < 16; i++ {
		c.advance(time.Second)
		r = w.Scan()
	}
	w.Flush()
	ev, _ := Export(&r, j, Query{BootID: "boot-7f3a"})
	b, _ := json.MarshalIndent(ev, "", "  ")
	fmt.Println(string(b))
	// Output:
	// {
	//   "schema": "clawdline.connection-health.evidence/1",
	//   "generated_at": "2026-10-20T12:00:16.04Z",
	//   "watchdog": {
	//     "rule": "watchdog/1",
	//     "wall": "2026-10-20T12:00:16.04Z",
	//     "mono_ns": 16040000000,
	//     "findings": [
	//       {
	//         "phase_id": 1,
	//         "source": "daemon",
	//         "stage": "idle",
	//         "class": "transport",
	//         "kind": "long_lived",
	//         "location": "cloud.transport.read_loop",
	//         "state": "idle",
	//         "age_ns": 16040000000,
	//         "active_ns": 16040000000,
	//         "progress": 0
	//       },
	//       {
	//         "phase_id": 2,
	//         "source": "daemon",
	//         "stage": "handler_started",
	//         "class": "read",
	//         "kind": "finite",
	//         "location": "cloud.link.read_handler",
	//         "correlation": "tr-01HZX3",
	//         "state": "blocked",
	//         "age_ns": 16000000000,
	//         "active_ns": 16040000000,
	//         "deadline_ns": 15000000000,
	//         "progress": 1
	//       }
	//     ],
	//     "overflow": {
	//       "total": 0,
	//       "critical": 0
	//     },
	//     "trace_coverage": "complete",
	//     "scan_seq": 16,
	//     "transitions": "complete",
	//     "transitions_lost_total": 0,
	//     "transitions_pending": 2
	//   },
	//   "journal": {
	//     "schema": "clawdline.connection-health.page/1",
	//     "generated_at": "2026-10-20T12:00:16.04Z",
	//     "boot_id": "boot-7f3a",
	//     "build_commit": "c9fe745db066",
	//     "records": [
	//       {
	//         "schema": "clawdline.connection-health.record/1",
	//         "producer_seq": 1,
	//         "event_id": "boot-7f3a-1",
	//         "boot_id": "boot-7f3a",
	//         "event": "boot",
	//         "wall": "2026-10-20T12:00:00Z",
	//         "mono_ns": 0,
	//         "build_commit": "c9fe745db066"
	//       },
	//       {
	//         "schema": "clawdline.connection-health.record/1",
	//         "producer_seq": 2,
	//         "event_id": "boot-7f3a-2",
	//         "boot_id": "boot-7f3a",
	//         "event": "slow_detected",
	//         "wall": "2026-10-20T12:00:05.04Z",
	//         "mono_ns": 5040000000,
	//         "source": "daemon",
	//         "stage": "handler_started",
	//         "class": "read",
	//         "kind": "finite",
	//         "location": "cloud.link.read_handler",
	//         "phase_id": 2,
	//         "correlation": "tr-01HZX3",
	//         "age_ns": 5000000000,
	//         "deadline_ns": 5000000000,
	//         "count": 1,
	//         "rule": "watchdog/1"
	//       },
	//       {
	//         "schema": "clawdline.connection-health.record/1",
	//         "producer_seq": 3,
	//         "event_id": "boot-7f3a-3",
	//         "boot_id": "boot-7f3a",
	//         "event": "stall_detected",
	//         "wall": "2026-10-20T12:00:15.04Z",
	//         "mono_ns": 15040000000,
	//         "source": "daemon",
	//         "stage": "handler_started",
	//         "class": "read",
	//         "kind": "finite",
	//         "location": "cloud.link.read_handler",
	//         "phase_id": 2,
	//         "correlation": "tr-01HZX3",
	//         "age_ns": 15000000000,
	//         "deadline_ns": 15000000000,
	//         "count": 1,
	//         "rule": "watchdog/1"
	//       }
	//     ],
	//     "coverage": {
	//       "quality": "complete",
	//       "first_retained_seq": 1,
	//       "last_seq": 3,
	//       "evicted_total": 0,
	//       "refused_total": 0,
	//       "truncated": false,
	//       "next_after_seq": 3,
	//       "durability": "memory_only_lost_on_restart"
	//     },
	//     "newest_age_ns": 1000000000
	//   },
	//   "wiring": [
	//     {
	//       "part": "active_phase_tracker",
	//       "status": "implemented"
	//     },
	//     {
	//       "part": "watchdog",
	//       "status": "implemented"
	//     },
	//     {
	//       "part": "memory_journal",
	//       "status": "implemented"
	//     },
	//     {
	//       "part": "daemon_runtime_per_boot",
	//       "status": "implemented"
	//     },
	//     {
	//       "part": "daemon_instrumentation_call_sites",
	//       "status": "partial"
	//     },
	//     {
	//       "part": "durable_segmented_journal",
	//       "status": "unimplemented"
	//     },
	//     {
	//       "part": "stack_capture_worker",
	//       "status": "unimplemented"
	//     },
	//     {
	//       "part": "local_health_cli_and_route",
	//       "status": "implemented"
	//     },
	//     {
	//       "part": "cloud_ingest",
	//       "status": "unimplemented"
	//     },
	//     {
	//       "part": "cloud_query",
	//       "status": "unimplemented"
	//     },
	//     {
	//       "part": "relay_and_viewer_producers",
	//       "status": "unimplemented"
	//     },
	//     {
	//       "part": "stage_location_map_per_build",
	//       "status": "partial"
	//     }
	//   ]
	// }
}

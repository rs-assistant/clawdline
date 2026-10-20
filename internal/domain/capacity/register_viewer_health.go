package capacity

// The browser's connection-health producer
// (web/console/src/health-viewer/producer.ts VIEWER_HEALTH_LIMITS). The
// Cloud-side numbers are the Cloud contract's §14.3 producer fixture; they
// live in each signed-in page that turned the producer on, and
// TestViewerHealthBoundsMatchTheProducer holds the two sides together.
const (
	ConsoleViewerHealthBodyBytes         = "console.viewer_health_body_bytes"
	ConsoleViewerHealthBatchRows         = "console.viewer_health_batch_rows"
	ConsoleViewerHealthBufferRows        = "console.viewer_health_buffer_rows"
	ConsoleViewerHealthSendEverySeconds  = "console.viewer_health_send_every_seconds"
	ConsoleViewerHealthCoalesceSeconds   = "console.viewer_health_coalesce_seconds"
	ConsoleViewerHealthHourlyRequests    = "console.viewer_health_hourly_requests"
	ConsoleViewerHealthHourlyBytes       = "console.viewer_health_hourly_bytes"
	ConsoleViewerHealthAttempts          = "console.viewer_health_attempts"
	ConsoleViewerHealthBackoffSeconds    = "console.viewer_health_backoff_seconds"
	ConsoleViewerHealthRetryAfterSeconds = "console.viewer_health_retry_after_seconds"
	ConsoleViewerHealthRequestSeconds    = "console.viewer_health_request_seconds"
	ConsoleViewerHealthElapsedSeconds    = "console.viewer_health_elapsed_seconds"
	ConsoleViewerHealthCountRows         = "console.viewer_health_count_rows"
)

// viewerHealthBounds is every console.viewer_health_* row.
func viewerHealthBounds() []Entry {
	return []Entry{
		{
			// One upload body (bodyBytes). A batch is cut to fit; a single
			// record over it is dropped and counted, never sent for a 413.
			Name: ConsoleViewerHealthBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 16384, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Records in one batch (batchEvents); the rest wait for the next.
			Name: ConsoleViewerHealthBatchRows, Class: Buffer, Unit: Rows,
			Limit: 50, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Records waiting in the page (bufferEvents). At the bound the
			// oldest goes and droppedOldest counts it.
			Name: ConsoleViewerHealthBufferRows, Class: Progress, Unit: Rows,
			Limit: 200, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The least time between two batches (sendEveryMs).
			Name: ConsoleViewerHealthSendEverySeconds, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// How long a first record waits for company before a batch is cut
			// (coalesceMs).
			Name: ConsoleViewerHealthCoalesceSeconds, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Requests per clock hour, retries included (hourlyRequests, the
			// Cloud's per-device bound). At it nothing is sent until the hour
			// turns; the buffer's own bound applies meanwhile.
			Name: ConsoleViewerHealthHourlyRequests, Class: Buffer, Unit: Rows,
			Limit: 60, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Request bytes per clock hour, retries included (hourlyBytes).
			Name: ConsoleViewerHealthHourlyBytes, Class: Buffer, Unit: Bytes,
			Limit: 262144, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Tries of one batch, the first included (attempts); then it is
			// dropped and droppedBatches counts it.
			Name: ConsoleViewerHealthAttempts, Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The longest wait between tries (backoffMs: 5, 20, 60 s).
			Name: ConsoleViewerHealthBackoffSeconds, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The longest Retry-After honored (retryAfterMaxMs).
			Name: ConsoleViewerHealthRetryAfterSeconds, Class: Cache, Unit: Seconds,
			Limit: 3600, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One upload's deadline (requestTimeoutMs); past it the request is
			// aborted and retried like a network failure.
			Name: ConsoleViewerHealthRequestSeconds, Class: Cache, Unit: Seconds,
			Limit: 10, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A stage's elapsed_ms is clamped to the Cloud's ceiling
			// (elapsedMaxMs) rather than refused.
			Name: ConsoleViewerHealthElapsedSeconds, Class: Buffer, Unit: Seconds,
			Limit: 3600, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A record's count is clamped to the Cloud's ceiling (countMax).
			Name: ConsoleViewerHealthCountRows, Class: Buffer, Unit: Rows,
			Limit: 1000000, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
	}
}

package connectionhealth

import (
	"testing"
	"time"
)

// The instrumentation cost a caller pays per finite operation.
func BenchmarkBeginProgressEnd(b *testing.B) {
	tr := NewTracker(NewSystemClock())
	s := handlerSpec()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h, _ := tr.Begin(s)
			_ = h.Progress(StageHandlerCompleted)
			h.End(true)
		}
	})
}

// One watchdog scan of a full table, with the journal attached.
func BenchmarkScanFullTable(b *testing.B) {
	c := NewSystemClock()
	tr := NewTracker(c)
	j, _ := NewJournal(c, "boot-a", "abc123")
	w := NewWatchdog(tr, j, c, time.Second)
	s := handlerSpec()
	for i := 0; i < MaxActiveSlots-MaxReservedSlots; i++ {
		_, _ = tr.Begin(s)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Scan()
	}
}

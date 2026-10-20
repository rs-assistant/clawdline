package healthjournal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

func benchRecord() connectionhealth.Record {
	return connectionhealth.Record{
		Event: connectionhealth.EventStallDetected, Source: connectionhealth.SourceDaemon,
		Stage: connectionhealth.StageHandlerStarted, Class: connectionhealth.ClassRead,
		Kind: connectionhealth.Finite, Location: "cloud.link.read_handler",
		Correlation: "tr-1", AgeNS: 15e9, Rule: connectionhealth.RuleVersion,
	}
}

// The foundation's own cost, the "store off" baseline.
func BenchmarkFoundationAppend(b *testing.B) {
	j, _ := connectionhealth.NewJournal(connectionhealth.NewSystemClock(), "boot-a", "b")
	r := benchRecord()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		j.Append(r)
	}
}

// Store on: the foundation append plus Offer of the stamped copy, with the
// worker writing to a real file underneath.
func BenchmarkFoundationAppendAndOffer(b *testing.B) {
	clock := connectionhealth.NewSystemClock()
	j, _ := connectionhealth.NewJournal(clock, "boot-a", "b")
	s, err := Open(Options{Dir: b.TempDir(), BootID: "boot-a", Build: "b", Clock: clock})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close(context.Background())
	r := benchRecord()
	var last uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j.Append(r)
		page, _ := j.Read(connectionhealth.Query{AfterSeq: last, Limit: 1})
		for _, x := range page.Records {
			s.Offer(x)
			last = x.Seq
		}
	}
	b.StopTimer()
	st := s.Status()
	b.ReportMetric(float64(st.Refused), "refused")
}

// Offer alone, against a worker that keeps up (flushed every 512).
func BenchmarkOffer(b *testing.B) {
	clock := connectionhealth.NewSystemClock()
	j, _ := connectionhealth.NewJournal(clock, "boot-a", "b")
	for i := 0; i < 511; i++ {
		j.Append(benchRecord())
	}
	page, _ := j.Read(connectionhealth.Query{Limit: 200})
	recs := page.Records
	s, err := Open(Options{Dir: b.TempDir(), BootID: "boot-a", Build: "b", Clock: clock})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close(context.Background())
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Offer(recs[i%len(recs)])
		if i%512 == 511 {
			b.StopTimer()
			s.Flush(ctx)
			b.StartTimer()
		}
	}
}

// End to end: offer 1,000 records and wait for them to be written and
// fsynced; reported per record.
func BenchmarkWriteAndSync1000(b *testing.B) {
	clock := connectionhealth.NewSystemClock()
	j, _ := connectionhealth.NewJournal(clock, "boot-a", "b")
	for i := 0; i < 199; i++ {
		j.Append(benchRecord())
	}
	page, _ := j.Read(connectionhealth.Query{Limit: 200})
	s, err := Open(Options{Dir: b.TempDir(), BootID: "boot-a", Build: "b", Clock: clock})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close(context.Background())
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k := 0; k < 1000; k++ {
			s.Offer(page.Records[k%len(page.Records)])
		}
		if _, err := s.Flush(ctx); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*1000), "ns/record")
	b.ReportMetric(float64(s.Status().Bytes)/float64(s.Status().Records), "bytes/record")
}

// One full 200-record page from a store holding 20,000 records.
func BenchmarkReadPage(b *testing.B) {
	clock := connectionhealth.NewSystemClock()
	j, _ := connectionhealth.NewJournal(clock, "boot-a", "b")
	for i := 0; i < 199; i++ {
		j.Append(benchRecord())
	}
	page, _ := j.Read(connectionhealth.Query{Limit: 200})
	s, err := Open(Options{Dir: b.TempDir(), BootID: "boot-a", Build: "b", Clock: clock})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close(context.Background())
	for k := 0; k < 20000; k++ {
		if _, err := s.Offer(page.Records[k%len(page.Records)]); err != nil {
			s.Flush(context.Background())
			k--
		}
	}
	s.Flush(context.Background())
	mid, _ := s.Read(Query{Limit: 200})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := s.Read(Query{Cursor: mid.Coverage.NextCursor, Limit: 200})
		if err != nil || len(p.Entries) != 200 {
			b.Fatal(err, len(p.Entries))
		}
	}
}

// TestMeasureFullStoreRecovery fills a store to its 100 MiB bound and times a
// reopen (the recovery scan) and a page read at the far end. It writes about
// 100 MiB, so it runs only when HEALTHJOURNAL_MEASURE names a directory.
func TestMeasureFullStoreRecovery(t *testing.T) {
	root := os.Getenv("HEALTHJOURNAL_MEASURE")
	if root == "" {
		t.Skip("set HEALTHJOURNAL_MEASURE to a scratch directory to measure a full store")
	}
	dir, err := os.MkdirTemp(root, "store-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	clock := connectionhealth.NewSystemClock()
	j, _ := connectionhealth.NewJournal(clock, "boot-a", "b")
	for i := 0; i < 199; i++ {
		j.Append(benchRecord())
	}
	page, _ := j.Read(connectionhealth.Query{Limit: 200})
	s, err := Open(Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	start := time.Now()
	for k := 0; s.Status().Bytes < MaxStoreBytes-MaxSegmentBytes; k++ {
		if _, err := s.Offer(page.Records[k%len(page.Records)]); err != nil {
			s.Flush(ctx)
		}
	}
	s.Flush(ctx)
	fill := time.Since(start)
	st := s.Status()
	s.Close(ctx)
	start = time.Now()
	s2, err := Open(Options{Dir: dir, BootID: "boot-b", Build: "b", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	open := time.Since(start)
	start = time.Now()
	p, err := s2.Read(Query{Cursor: cursorOf(uint64(st.Segments), 0), Limit: 200})
	read := time.Since(start)
	s2.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("filled %d records, %d bytes, %d segments in %v (%.0f ns/record); reopen scan %v; page of %d at the newest segment %v",
		st.Records, st.Bytes, st.Segments, fill, float64(fill.Nanoseconds())/float64(st.Records), open, len(p.Entries), read)
}

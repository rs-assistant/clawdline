//go:build unix

package connectionhealth

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"
)

// park holds a goroutine depth frames deep until release closes.
//
//go:noinline
func park(depth int, release <-chan struct{}, ready *sync.WaitGroup) {
	if depth > 0 {
		park(depth-1, release, ready)
		return
	}
	ready.Done()
	<-release
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// stwOther is the runtime's own non-GC stop-the-world pause histogram, as
// count and the upper bound of the highest non-empty bucket.
func stwOther() (count uint64, max float64) {
	s := []metrics.Sample{{Name: "/sched/pauses/total/other:seconds"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindFloat64Histogram {
		return 0, -1
	}
	h := s[0].Value.Float64Histogram()
	for i, n := range h.Counts {
		count += n
		if n > 0 {
			max = h.Buckets[i+1]
		}
	}
	return count, max
}

// probeLateness runs a 200 µs ticker beside fn and returns the worst
// lateness it saw: how long another goroutine could not run.
func probeLateness(fn func()) time.Duration {
	stop := make(chan struct{})
	worst := make(chan time.Duration, 1)
	go func() {
		const every = 200 * time.Microsecond
		var w time.Duration
		next := time.Now().Add(every)
		for {
			select {
			case <-stop:
				worst <- w
				return
			default:
			}
			time.Sleep(time.Until(next))
			if late := time.Since(next); late > w {
				w = late
			}
			next = time.Now().Add(every)
		}
	}()
	time.Sleep(5 * time.Millisecond)
	fn()
	time.Sleep(5 * time.Millisecond)
	close(stop)
	return <-worst
}

// TestMeasureCaptureCost is the measurement behind capture being on by
// default (docs/admin-connection-health.md "Stall capture"). It runs only
// with CLAWDLINE_CAPTURE_MEASURE=1 and prints real numbers for a process
// holding N parked goroutines, each 12 frames deep: control (capture off,
// the same probe and loop) against on, per capture.
func TestMeasureCaptureCost(t *testing.T) {
	if os.Getenv("CLAWDLINE_CAPTURE_MEASURE") != "1" {
		t.Skip("set CLAWDLINE_CAPTURE_MEASURE=1 to measure")
	}
	const rounds = 20
	fmt.Printf("go %s GOMAXPROCS=%d GOOS=%s GOARCH=%s rounds=%d\n", runtime.Version(), runtime.GOMAXPROCS(0), runtime.GOOS, runtime.GOARCH, rounds)
	fmt.Printf("%-6s %-7s %10s %10s %10s %10s %12s %12s %8s %10s %9s %9s\n",
		"gor", "mode", "p50_wall", "max_wall", "cpu/op", "allocs/op", "bytes/op", "probe_late", "stw_n", "stw_max", "raw_kib", "out_kib")
	for _, n := range []int{100, 1000, 5000} {
		release := make(chan struct{})
		var ready sync.WaitGroup
		ready.Add(n)
		for i := 0; i < n; i++ {
			go park(12, release, &ready)
		}
		ready.Wait()
		for _, on := range []bool{false, true} {
			c := NewCapturer(NewSystemClock(), "boot-m", "measure")
			c.SetEnabled(on)
			c.raw = make([]byte, MaxCaptureRawBytes)
			trig := CaptureTrigger{PhaseID: 1, Location: LocWriterLock}
			var walls []time.Duration
			var ms0, ms1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&ms0)
			stwN0, _ := stwOther()
			cpu0 := cpuTime()
			var late time.Duration
			for r := 0; r < rounds; r++ {
				l := probeLateness(func() {
					start := time.Now()
					if on {
						c.take(trig)
					} else {
						// Control: the same request path, refused as off.
						c.Request(trig)
					}
					walls = append(walls, time.Since(start))
				})
				if l > late {
					late = l
				}
			}
			cpu := cpuTime() - cpu0
			runtime.ReadMemStats(&ms1)
			stwN1, stwMax := stwOther()
			sort.Slice(walls, func(i, j int) bool { return walls[i] < walls[j] })
			mode := "off"
			raw, out := 0, 0
			if on {
				mode = "on"
				if got, ok := c.Latest(); ok {
					raw = got.RawBytes
					out = len(mustJSON(t, got))
				}
			}
			fmt.Printf("%-6d %-7s %10s %10s %10s %10d %12d %12s %8d %10.6f %9d %9d\n", n, mode,
				walls[len(walls)/2], walls[len(walls)-1], cpu/rounds,
				(ms1.Mallocs-ms0.Mallocs)/rounds, (ms1.TotalAlloc-ms0.TotalAlloc)/rounds, late,
				stwN1-stwN0, stwMax, raw/1024, out/1024)
		}
		close(release)
		time.Sleep(20 * time.Millisecond)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

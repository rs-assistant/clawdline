package projects

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepoLookupKeepsPlaceListResponsiveWhenOneOpenStalls(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	var calls atomic.Int32
	lookup := NewRepoLookup(func(path string) string {
		calls.Add(1)
		if path == "/blocked" {
			<-blocked
		}
		return "example.com/team/" + path[1:]
	})
	places := []Place{{Path: "/blocked"}, {Path: "/healthy"}}
	start := time.Now()
	got := lookup.Resolve(places)
	if elapsed := time.Since(start); elapsed > 2*RepoLookupWait {
		t.Fatalf("one blocked Git config held the list for %s", elapsed)
	}
	if got["/healthy"] != "example.com/team/healthy" {
		t.Fatalf("healthy repository was lost while another stalled: %v", got)
	}
	start = time.Now()
	got = lookup.Resolve(places)
	if elapsed := time.Since(start); elapsed > RepoLookupWait/2 {
		t.Fatalf("a repeated request waited on the same blocked read for %s", elapsed)
	}
	if got["/healthy"] != "example.com/team/healthy" || calls.Load() != 2 {
		t.Fatalf("completed result not cached or blocked read restarted: %v, calls=%d", got, calls.Load())
	}
}

func TestRepoLookupBoundsConcurrentBlockedReads(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	var active, peak atomic.Int32
	lookup := NewRepoLookup(func(string) string {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		<-blocked
		active.Add(-1)
		return ""
	})
	places := make([]Place, MaxRepoLookups)
	for i := range places {
		places[i].Path = fmt.Sprintf("/blocked-%d", i)
	}
	lookup.Resolve(places)
	lookup.Resolve(places)
	if got := peak.Load(); got != MaxRepoReaders {
		t.Fatalf("blocked reads reached %d workers; want %d", got, MaxRepoReaders)
	}
	lookup.mu.Lock()
	entries := len(lookup.entries)
	lookup.mu.Unlock()
	if entries != MaxRepoLookups {
		t.Fatalf("remembered %d blocked paths; want %d", entries, MaxRepoLookups)
	}
}

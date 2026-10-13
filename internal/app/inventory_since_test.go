package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// The session list built right after a press must come from a scan begun after
// it. A scan in flight that began before is waited out, and a held scan inside
// its freshness window is not served.
func TestAReadingSinceAMomentIsAScanBegunAfterIt(t *testing.T) {
	var scans atomic.Int32
	release := make(chan struct{})
	reading := NewInventoryReading(func(context.Context) session.Inventory {
		n := scans.Add(1)
		if n == 2 {
			<-release
		}
		return session.Inventory{Sessions: []session.Session{{ID: "%7", Line: string(rune('0' + n))}},
			ObservedAt: time.Now()}
	}, time.Minute)
	ctx := context.Background()
	if got := reading.Fast(ctx); len(got.Sessions) != 1 || got.Sessions[0].Line != "1" {
		t.Fatalf("first reading %+v", got.Sessions)
	}
	if got := reading.Fast(ctx); got.Sessions[0].Line != "1" {
		t.Fatalf("control: inside its window the held scan is served; got %q", got.Sessions[0].Line)
	}
	// A scan begins (somebody's Fresh), and then the press happens.
	go reading.Fresh(ctx)
	for scans.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	pressed := time.Now()
	answered := make(chan string, 1)
	go func() { answered <- reading.Since(ctx, pressed).Sessions[0].Line }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	select {
	case line := <-answered:
		if line != "3" {
			t.Fatalf("the reading after the press came from scan %s; want one begun after it", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reading after the press never came back")
	}
}

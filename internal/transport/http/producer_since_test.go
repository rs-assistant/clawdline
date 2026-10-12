package http

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A look right after a press must read the machine after the press. A build
// that began before it is waited out, never answered as the fresh list.
func TestAFreshListIsNeverABuildThatBeganBeforeTheMoment(t *testing.T) {
	var reads atomic.Int32
	release := make(chan struct{})
	l := latest[int32]{build: func(context.Context) (int32, error) {
		n := reads.Add(1)
		if n == 1 {
			<-release
		}
		return n, nil
	}}
	// A build is in flight: some stream's tick, begun before the press.
	go func() { _, _ = l.refresh() }()
	for reads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	pressed := time.Now()
	answered := make(chan int32, 1)
	go func() { v, _ := l.since(pressed); answered <- v }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	select {
	case v := <-answered:
		if v != 2 {
			t.Fatalf("the look after the press was answered build %d; want a build begun after it", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the look after the press never came back")
	}
	if v, _ := l.since(pressed); v != 2 {
		t.Fatalf("a build begun after the moment is reused: got %d", v)
	}
}

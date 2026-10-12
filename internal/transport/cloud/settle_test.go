package cloud

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A press from a page used to wait for the next pass to be seen landing: the
// machine answered it in well under a second, and the row showing its effect
// went out up to SnapshotInterval later. Settle looks again at once, with the
// list built fresh, so the changed row goes out without that wait.
func TestAPressIsShownLandingWithoutWaitingForTheNextPass(t *testing.T) {
	out := &collector{}
	before := scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"})
	after := scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"},
		scanRow{id: "%20", tty: "ttys002", backend: "tmux"})
	publisher := newPublisher(&fixedRouter{body: before}, out)
	// No pass of its own comes inside this test.
	publisher.Every = time.Hour
	var fresh atomic.Int32
	publisher.Fresh = func(context.Context) ([]byte, bool) {
		fresh.Add(1)
		return []byte(after), true
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = publisher.Run(ctx); close(done) }()
	waitFor(t, "the first pass", func() bool { return len(legacyChannels(out.channels())) >= 3 })
	out.reset()

	published := func() bool { return strings.Contains(strings.Join(out.channels(), " "), "s/mac-01/%2520") }
	time.Sleep(300 * time.Millisecond)
	if published() || fresh.Load() != 0 {
		t.Fatalf("without a press the row went out anyway: %v", out.channels())
	}
	started := time.Now()
	publisher.Settle()
	publisher.Settle()
	waitFor(t, "the pressed row", published)
	if took := time.Since(started); took > time.Second {
		t.Fatalf("the row went out %v after the press", took)
	}
	waitFor(t, "the second look", func() bool { return fresh.Load() == int32(len(SettleDelays)) })
	time.Sleep(SettleDelays[len(SettleDelays)-1])
	if n := fresh.Load(); n != int32(len(SettleDelays)) {
		t.Fatalf("two presses in a row looked %d times; want one sequence of %d", n, len(SettleDelays))
	}
	cancel()
	<-done
}

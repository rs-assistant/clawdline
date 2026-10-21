package cloud

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
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

// A page with a Session open and waiting asks for that one row when its
// question has not come. The row has not changed, so no pass would send it
// again; the whole restatement it used to ask waited for the next pass.
func TestOneSessionRowIsStatedNowEvenUnchanged(t *testing.T) {
	out := &collector{}
	body := scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"},
		scanRow{id: "%20", tty: "ttys002", backend: "tmux"})
	publisher := newPublisher(&fixedRouter{body: body}, out)
	publisher.Every = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = publisher.Run(ctx); close(done) }()
	waitFor(t, "the first pass", func() bool { return len(legacyChannels(out.channels())) >= 4 })
	out.reset()

	started := time.Now()
	stated, refusal := publisher.StateRow(ctx, "viewer-01", "%19")
	if refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("the row was stated %v after it was asked", took)
	}
	if strings.Join(stated.IDs, ",") != "%19" {
		t.Fatalf("answered %+v", stated)
	}
	// The answer carries the row it put on s/, byte for byte, so a page that
	// was not subscribed to that channel at the moment still has it.
	var carried struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(stated.Row, &carried); err != nil || carried.Session.ID != "%19" {
		t.Fatalf("the answer carried %s (%v)", stated.Row, err)
	}
	var answered map[string]any
	_ = json.Unmarshal(stated.Row, &answered)
	if sent := out.payload(t, "s/mac-01/%2519"); !reflect.DeepEqual(sent, answered) {
		t.Fatalf("the answer carried %v; s/ was given %v", answered, sent)
	}
	channels := strings.Join(out.channels(), " ")
	if !strings.Contains(channels, "s/mac-01/%2519") || strings.Contains(channels, "s/mac-01/%2520") {
		t.Fatalf("published %v; want the one row asked for", channels)
	}
	if _, refusal := publisher.StateRow(ctx, "viewer-01", "%19"); refusal == nil || refusal.Code != "cloud_read_busy" {
		t.Fatalf("the same viewer asking again at once was answered %+v", refusal)
	}
	if stated, refusal := publisher.StateRow(ctx, "viewer-01", "%99"); refusal != nil || len(stated.IDs) != 0 || stated.Row != nil {
		t.Fatalf("a row the machine does not have was answered %+v %+v", stated, refusal)
	}
	cancel()
	<-done
}

// Opening a Session asks for its row so the list beside the pane agrees with
// it. The list is drawn from the content-free ss/ row, which, unchanged, no
// pass sends again; an ask that restated only the s/ row left a list that had
// missed the last ss/ row saying "waiting for you" next to a finished pane.
func TestAnAskedRowRestatesItsStatusRowToo(t *testing.T) {
	out := &collector{}
	body := scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"},
		scanRow{id: "%20", tty: "ttys002", backend: "tmux"})
	publisher := newPublisher(&fixedRouter{body: body}, out)
	publisher.Every = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = publisher.Run(ctx); close(done) }()
	waitFor(t, "the first status pass", func() bool {
		return strings.Contains(strings.Join(out.channels(), " "), "ss/mac-01/%2519")
	})
	out.reset()

	if _, refusal := publisher.StateRow(ctx, "viewer-01", "%19"); refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	var status []string
	for _, channel := range out.channels() {
		if strings.HasPrefix(channel, "ss/") {
			status = append(status, channel)
		}
	}
	if got := strings.Join(status, " "); got != "ss/mac-01/%2519" {
		t.Fatalf("the ask restated status rows %q; want only the asked Session's", got)
	}
	cancel()
	<-done
}

// A page asks for a waiting row because its question has not come. On
// 2026-10-14 the phone reported `rich_state=waiting rich_options=0` five times:
// the row it was sent had been built from a screen held from before the
// question was drawn. The ask makes that Session's screen a capture first, and
// the fresh read comes after it.
func TestAnAskedRowLooksAtItsScreenBeforeItsFreshRead(t *testing.T) {
	out := &collector{}
	body := scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"})
	publisher := newPublisher(&fixedRouter{body: body}, out)
	publisher.Every = time.Hour
	var mu sync.Mutex
	var order []string
	publisher.Look = func(id string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "look "+id)
	}
	publisher.Fresh = func(context.Context) ([]byte, bool) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "fresh")
		return []byte(body), true
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = publisher.Run(ctx); close(done) }()
	waitFor(t, "the first pass", func() bool { return len(legacyChannels(out.channels())) >= 3 })
	if _, refusal := publisher.StateRow(ctx, "viewer-01", "%19"); refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	mu.Lock()
	got := strings.Join(order, ", ")
	mu.Unlock()
	if got != "look %19, fresh" {
		t.Fatalf("the ask did %q; want the screen looked at, then the fresh read", got)
	}
	cancel()
	<-done
}

func TestATaskChangePublishesSessionWorkBeforeThePollingPass(t *testing.T) {
	out := &collector{}
	before := scanBody(t, true, map[string]bool{"tmux": true}, scanRow{id: "%19", tty: "ttys001", backend: "tmux"})
	after := scanBody(t, true, map[string]bool{"tmux": true}, scanRow{id: "%19", tty: "ttys001", backend: "tmux"}, scanRow{id: "%20", tty: "ttys002", backend: "tmux"})
	p := newPublisher(&fixedRouter{body: before}, out)
	p.Every = time.Hour
	var reads atomic.Int32
	p.Fresh = func(context.Context) ([]byte, bool) { reads.Add(1); return []byte(after), true }
	link := &Link{publisher: p}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = p.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, "initial publication", func() bool { return len(legacyChannels(out.channels())) >= 3 })
	if reads.Load() != 0 {
		t.Fatal("control: an unchanged list was refreshed")
	}
	out.reset()
	link.TaskListChanged()
	link.TaskListChanged()
	waitFor(t, "task change publication", func() bool { return strings.Contains(strings.Join(out.channels(), " "), "s/mac-01/%2520") })
	if !link.takeTaskListMoved() {
		t.Fatal("task list dirty signal was lost")
	}
}

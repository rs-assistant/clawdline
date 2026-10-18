package cloud

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	peercontract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

// The notice loop opens the peer store once for its life and closes it when
// the line stops, however many passes it makes; before, every pass opened and
// closed it twice.
func TestTheNoticeLoopOpensThePeerStoreOnce(t *testing.T) {
	keys, err := cloudkeys.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var opened []*peerstore.Store
	wasEvery, wasOpen := taskNoticeEvery, openPeerStore
	t.Cleanup(func() { taskNoticeEvery, openPeerStore = wasEvery, wasOpen })
	taskNoticeEvery = 5 * time.Millisecond
	openPeerStore = func(dir string) (*peerstore.Store, error) {
		st, err := peerstore.Open(dir)
		if err == nil {
			mu.Lock()
			opened = append(opened, st)
			mu.Unlock()
		}
		return st, err
	}
	l := &Link{keys: keys, opts: LinkOptions{Now: time.Now,
		PeerTaskNotice:        func(context.Context, peercontract.TaskStart) error { return nil },
		PeerTaskMessageNotice: func(context.Context, peercontract.TaskEvent) error { return nil },
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); l.runTaskNotices(ctx) }()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 {
		t.Fatalf("the loop opened the peer store %d times, want once", len(opened))
	}
	if _, err := opened[0].PendingTaskFinishes(context.Background()); err == nil {
		t.Fatal("the peer store is still open after the line stopped")
	}
}

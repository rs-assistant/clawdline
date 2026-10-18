package updater

import (
	"context"
	"testing"
	"time"
)

// While the app stays open, the daemon's poll asks after the pids it saw
// instead of listing every process; a quit is seen on the next poll, and a
// listing is trusted no longer than AppRelistSecondsLimit.
func TestAppWatchListsProcessesOnlyWhenThePidsAreGone(t *testing.T) {
	const bundle = "/Applications/Clawdline Next.app"
	now := time.Unix(1_800_000_000, 0)
	lists := 0
	open := true
	env := Env{Now: func() time.Time { return now }, Run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name != "ps" {
			t.Fatalf("ran %s", name)
		}
		lists++
		if !open {
			return []byte("    1 /sbin/launchd\n"), nil
		}
		return []byte("    1 /sbin/launchd\n  812 " + bundle + "/Contents/MacOS/Clawdline Next\n"), nil
	}}
	alive := map[int]bool{812: true}
	w := &appWatch{env: env, alive: func(pid int) bool { return alive[pid] }}
	look := func(want bool) {
		t.Helper()
		running, err := w.running(context.Background(), bundle)
		if err != nil || running != want {
			t.Fatalf("running %v %v, want %v", running, err, want)
		}
	}
	// A minute of polls while the app is open lists processes once.
	for i := 0; i < 29; i++ {
		look(true)
		now = now.Add(AppSwapPollSecondsLimit * time.Second)
	}
	if lists != 1 {
		t.Fatalf("listed processes %d times in a minute of an open app", lists)
	}
	// An old listing is not trusted, even with its pid alive.
	now = now.Add(AppRelistSecondsLimit * time.Second)
	look(true)
	if lists != 2 {
		t.Fatalf("a %ds old listing was trusted", AppRelistSecondsLimit)
	}
	// The app quits: the very next poll lists again and finds it gone.
	open, alive[812] = false, false
	now = now.Add(AppSwapPollSecondsLimit * time.Second)
	look(false)
	if lists != 3 {
		t.Fatalf("a quit was not listed at once: %d listings", lists)
	}
	// Nothing to watch: every poll lists, as it did before.
	look(false)
	if lists != 4 {
		t.Fatalf("no pids to ask after, yet %d listings", lists)
	}
}

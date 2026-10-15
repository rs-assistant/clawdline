package projects

import (
	"sync"
	"time"
)

// A repository read can wait indefinitely for macOS to grant a newly signed
// release access to a protected checkout. The start sheet and the installer's
// tmux check still need their place list in that case.
const (
	MaxRepoLookups     = 40
	MaxRepoReaders     = 4
	MaxRepoWaitSeconds = 1
	RepoLookupWait     = time.Duration(MaxRepoWaitSeconds) * time.Second
)

type repoLookupEntry struct {
	done  chan struct{}
	value string
	ready bool
}

// RepoLookup keeps the optional origin metadata outside the HTTP request.
// Pending reads keep their slot so repeated requests cannot start another
// blocked read for the same path. Completed entries may be evicted when the
// place list changes.
type RepoLookup struct {
	mu      sync.Mutex
	entries map[string]*repoLookupEntry
	order   []string
	workers chan struct{}
	read    func(string) string
}

func NewRepoLookup(read func(string) string) *RepoLookup {
	return &RepoLookup{
		entries: make(map[string]*repoLookupEntry),
		workers: make(chan struct{}, MaxRepoReaders),
		read:    read,
	}
}

// Resolve waits briefly for new lookups, then returns the values that arrived.
// A later request reads completed results without waiting on an older stall.
func (l *RepoLookup) Resolve(places []Place) map[string]string {
	wanted := make(map[string]bool, len(places))
	for _, p := range places {
		wanted[p.Path] = true
	}
	l.mu.Lock()
	var started []*repoLookupEntry
	for _, p := range places {
		if _, exists := l.entries[p.Path]; exists {
			continue
		}
		for len(l.entries) >= MaxRepoLookups {
			if !l.evictCompleted(wanted) {
				break
			}
		}
		if len(l.entries) >= MaxRepoLookups {
			continue
		}
		e := &repoLookupEntry{done: make(chan struct{})}
		l.entries[p.Path] = e
		l.order = append(l.order, p.Path)
		started = append(started, e)
		go l.load(p.Path, e)
	}
	l.mu.Unlock()

	if len(started) > 0 {
		deadline := time.NewTimer(RepoLookupWait)
		defer deadline.Stop()
		for _, e := range started {
			select {
			case <-e.done:
			case <-deadline.C:
				return l.ready(places)
			}
		}
	}
	return l.ready(places)
}

// evictCompleted is called with mu held. A pending read is never evicted:
// evicting it would allow a second blocked read of the same path.
func (l *RepoLookup) evictCompleted(wanted map[string]bool) bool {
	for i, path := range l.order {
		e := l.entries[path]
		if wanted[path] || !e.ready {
			continue
		}
		delete(l.entries, path)
		l.order = append(l.order[:i], l.order[i+1:]...)
		return true
	}
	return false
}

func (l *RepoLookup) load(path string, e *repoLookupEntry) {
	l.workers <- struct{}{}
	value := l.read(path)
	<-l.workers
	l.mu.Lock()
	e.value, e.ready = value, true
	close(e.done)
	l.mu.Unlock()
}

func (l *RepoLookup) ready(places []Place) map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]string, len(places))
	for _, p := range places {
		if e := l.entries[p.Path]; e != nil && e.ready {
			out[p.Path] = e.value
		}
	}
	return out
}

// Counts is the capacity beat's view of retained paths and active reads.
func (l *RepoLookup) Counts() (entries, readers int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int64(len(l.entries)), int64(len(l.workers))
}

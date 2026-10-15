package http

import (
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

// Project setup is optional metadata on the start-place list. A filesystem
// open can wait indefinitely on a protected checkout, so it must not hold the
// list (or the installer's first-session check) while it waits.
const (
	MaxProjectSetupLookups     = 40
	MaxProjectSetupReaders     = 4
	MaxProjectSetupWaitSeconds = 1
	MaxProjectSetupAgeSeconds  = 60
)

type setupLookupKey struct {
	path, repo string
	icon       icon.Source
}

type setupLookupEntry struct {
	done      chan struct{}
	value     *contract.ProjectSetup
	ready     bool
	completed time.Time
}

type setupLookup struct {
	mu      sync.Mutex
	entries map[setupLookupKey]*setupLookupEntry
	order   []setupLookupKey
	workers chan struct{}
	read    func(string, string, icon.Source) *contract.ProjectSetup
}

func newSetupLookup(read func(string, string, icon.Source) *contract.ProjectSetup) *setupLookup {
	return &setupLookup{
		entries: make(map[setupLookupKey]*setupLookupEntry),
		workers: make(chan struct{}, MaxProjectSetupReaders),
		read:    read,
	}
}

// Resolve starts all new reads before waiting once. An unfinished read keeps
// its slot, so repeat requests cannot spawn another blocked open for it.
func (l *setupLookup) Resolve(keys []setupLookupKey) map[setupLookupKey]*contract.ProjectSetup {
	wanted := make(map[setupLookupKey]bool, len(keys))
	for _, key := range keys {
		wanted[key] = true
	}
	l.mu.Lock()
	var started []*setupLookupEntry
	now := time.Now()
	for _, key := range keys {
		if e := l.entries[key]; e != nil {
			if !e.ready || now.Sub(e.completed) < MaxProjectSetupAgeSeconds*time.Second {
				continue
			}
			l.remove(key)
		}
		for len(l.entries) >= MaxProjectSetupLookups {
			if !l.evictCompleted(wanted) {
				break
			}
		}
		if len(l.entries) >= MaxProjectSetupLookups {
			continue
		}
		e := &setupLookupEntry{done: make(chan struct{})}
		l.entries[key] = e
		l.order = append(l.order, key)
		started = append(started, e)
		go l.load(key, e)
	}
	l.mu.Unlock()

	if len(started) > 0 {
		deadline := time.NewTimer(MaxProjectSetupWaitSeconds * time.Second)
		defer deadline.Stop()
		for _, e := range started {
			select {
			case <-e.done:
			case <-deadline.C:
				return l.ready(keys)
			}
		}
	}
	return l.ready(keys)
}

// remove and evictCompleted are called with mu held.
func (l *setupLookup) remove(key setupLookupKey) {
	delete(l.entries, key)
	for i, existing := range l.order {
		if existing == key {
			l.order = append(l.order[:i], l.order[i+1:]...)
			return
		}
	}
}

func (l *setupLookup) evictCompleted(wanted map[setupLookupKey]bool) bool {
	for _, key := range l.order {
		if e := l.entries[key]; e != nil && e.ready && !wanted[key] {
			l.remove(key)
			return true
		}
	}
	return false
}

func (l *setupLookup) load(key setupLookupKey, e *setupLookupEntry) {
	l.workers <- struct{}{}
	value := l.read(key.path, key.repo, key.icon)
	<-l.workers
	l.mu.Lock()
	e.value, e.ready, e.completed = value, true, time.Now()
	close(e.done)
	l.mu.Unlock()
}

func (l *setupLookup) ready(keys []setupLookupKey) map[setupLookupKey]*contract.ProjectSetup {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[setupLookupKey]*contract.ProjectSetup, len(keys))
	for _, key := range keys {
		if e := l.entries[key]; e != nil && e.ready {
			out[key] = e.value
		}
	}
	return out
}

func (l *setupLookup) Counts() (entries, readers int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int64(len(l.entries)), int64(len(l.workers))
}

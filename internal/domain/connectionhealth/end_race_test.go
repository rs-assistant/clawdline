package connectionhealth

import (
	"sync"
	"testing"
)

// A Progress that lands between End reading the phase and clearing it used
// to make End give up, leaving the slot busy and the phase "active" forever.
// The hook puts the Progress exactly there, so this fails every run, not by
// luck of the scheduler.
func TestEndThatRacesProgressStillReleasesTheSlot(t *testing.T) {
	c := newFakeClock()
	tr := NewTracker(c)
	h, err := tr.Begin(handlerSpec())
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	testHookEndLoaded = func() {
		if !fired {
			fired = true
			if err := h.Progress(StageHandlerCompleted); err != nil {
				t.Error(err)
			}
		}
	}
	defer func() { testHookEndLoaded = nil }()
	h.End(true)
	testHookEndLoaded = nil
	if phases, _ := tr.Snapshot(); len(phases) != 0 {
		t.Fatalf("ended phase still active: %+v", phases)
	}
	if tr.slots[h.slot].busy.Load() {
		t.Fatal("slot still claimed after End")
	}
	completed, failed := tr.Counts()
	if completed[ClassRead] != 1 || failed[ClassRead] != 0 {
		t.Fatalf("counted %v completed, %v failed", completed, failed)
	}
}

// A stale handle from an earlier generation of a slot must not clear the
// newer phase that reused it, and ending twice counts once.
func TestRepeatedEndAndSlotReuseCountOnce(t *testing.T) {
	c := newFakeClock()
	tr := NewTracker(c)
	s := handlerSpec()
	s.Critical = true // reserved slots: few enough to force reuse
	first, err := tr.Begin(s)
	if err != nil {
		t.Fatal(err)
	}
	first.End(true)
	first.End(false)
	var reused Handle
	for i := 0; i < MaxReservedSlots; i++ {
		h, err := tr.Begin(s)
		if err != nil {
			t.Fatal(err)
		}
		if h.slot == first.slot {
			reused = h
		}
	}
	if !reused.Recorded() {
		t.Fatal("slot was not reused")
	}
	first.End(false)
	_ = first.Progress(StageSpoolQueued)
	phases, _ := tr.Snapshot()
	found := false
	for _, p := range phases {
		if p.ID == reused.id {
			found = p.Spec.Stage == s.Stage && p.Progress == 0
		}
	}
	if !found || !tr.slots[first.slot].busy.Load() {
		t.Fatal("stale handle touched the newer phase in its slot")
	}
	completed, failed := tr.Counts()
	if completed[ClassRead] != 1 || failed[ClassRead] != 0 {
		t.Fatalf("counted %v completed, %v failed", completed, failed)
	}
}

// End and Progress from many goroutines on the same handles: every slot is
// released, every phase counted exactly once.
func TestSimultaneousEndAndProgressReleaseEverySlot(t *testing.T) {
	tr := NewTracker(newFakeClock())
	const rounds = 2000
	for r := 0; r < rounds; r++ {
		h, err := tr.Begin(handlerSpec())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(2)
			go func() { defer wg.Done(); _ = h.Progress(StageSpoolQueued) }()
			go func(ok bool) { defer wg.Done(); h.End(ok) }(g%2 == 0)
		}
		wg.Wait()
		if tr.slots[h.slot].busy.Load() || tr.slots[h.slot].phase.Load() != nil {
			t.Fatalf("round %d: slot %d left claimed", r, h.slot)
		}
	}
	completed, failed := tr.Counts()
	if got := completed[ClassRead] + failed[ClassRead]; got != rounds {
		t.Fatalf("counted %d outcomes for %d phases", got, rounds)
	}
}

package auth

import (
	"errors"
	"strings"
	"testing"
)

// A reset from this machine forgets the wrong codes and requests that kept
// pairing closed, ends the open pairing, and says what it forgot in the audit.
func TestResetPairingReopensWhatWrongCodesClosed(t *testing.T) {
	store := &memStore{}
	a, _ := newAuthority(t, store)
	started, _ := a.BeginPairing("someone else")
	code := codeFor(t, a, started.ID)
	for i := 0; i < PairingGuesses; i++ {
		_, _ = a.ConfirmPairing(started.ID, wrong(code))
	}
	if _, err := a.BeginPairing("me"); !errors.Is(err, ErrPairingLocked) {
		t.Fatalf("after five wrong codes: %v", err)
	}
	if wrongCodes, requests := a.ResetPairing(); wrongCodes != PairingGuesses || requests != 1 {
		t.Fatalf("reset forgot %d wrong codes and %d requests; want %d and 1", wrongCodes, requests, PairingGuesses)
	}
	if r, _ := a.ConfirmPairing(started.ID, code); r.Kind != Expired {
		t.Fatalf("the pairing open before the reset still pairs: %+v", r)
	}
	mine, err := a.BeginPairing("me")
	if err != nil {
		t.Fatalf("after the reset: %v", err)
	}
	if r, _ := a.ConfirmPairing(mine.ID, codeFor(t, a, mine.ID)); r.Kind != Paired {
		t.Fatalf("pairing after the reset: %+v", r)
	}
	audit := strings.Join(store.audit, "\n")
	if !strings.Contains(audit, "pair.reset") || !strings.Contains(audit, "wrong_codes=5") {
		t.Fatalf("audit: %v", store.audit)
	}
}

package cloud

import (
	"bytes"
	"testing"
)

// Revoking a browser through Pairing.Revoke moves the content key to a new
// epoch, so the revoked browser cannot open what is sealed after it, and the
// next pairing hands out the new key. Before 2026-10-22 a revocation only wrote
// the pin refusal, and the revoked browser kept the one key this machine
// sealed everything with. The rotation itself, the rekey of the browsers that
// stay and the journal are upstream's (rekey_runtime_test.go); this holds the
// route's answer to what the epoch did.
func TestRevokingABrowserReplacesTheContentKeyItHeld(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	l.pairing = &Pairing{Pinned: l.pinned}
	l.pairing.RevokeContent = l.revokeContentViewer
	l.pairing.ContentState = l.contentEpoch.Current
	before, err := l.contentEpoch.Current()
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := l.pairing.Revoke("A")
	if err != nil || !outcome.Revoked || !outcome.SecretReplaced {
		t.Fatalf("the revocation did not replace the content key: %+v %v", outcome, err)
	}
	after, err := l.contentEpoch.Current()
	if err != nil || after.Epoch != before.Epoch+1 || bytes.Equal(after.Key.Bytes(), before.Key.Bytes()) {
		t.Fatalf("the content key did not move: %d -> %d %v", before.Epoch, after.Epoch, err)
	}
	if refused, _ := l.pinned.Refused("A"); !refused {
		t.Fatal("the revoked browser is not refused")
	}
	again, err := l.pairing.Revoke("A")
	if err != nil || again.Revoked || again.SecretReplaced {
		t.Fatalf("revoking twice answered %+v %v", again, err)
	}
	if now, _ := l.contentEpoch.Current(); now.Epoch != after.Epoch {
		t.Fatal("a revocation that changed nothing moved the key")
	}
}

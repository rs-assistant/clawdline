package cloud

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// Revoking a browser replaces the content key every browser paired here was
// handed, and the line is rebuilt on it. Before 2026-10-22 a revocation only
// wrote the pin refusal: the revoked browser kept the one secret this machine
// seals everything with, and went on reading whatever was sealed after it.
func TestRevokingABrowserReplacesTheContentKeyItHeld(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{"cloud_enabled":true}`)
	writeIdentity(t, dir, "")
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if link.relay == nil || link.Pairing() == nil {
		t.Fatalf("the link was not built: %s", link.Status().LastError)
	}
	before := link.relay.Secret.Bytes()
	revoked, kept := pairTestViewer(t, link, "dev_revoked"), pairTestViewer(t, link, "dev_kept")

	outcome, err := link.Pairing().Revoke("dev_revoked")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !outcome.Revoked || !outcome.SecretReplaced {
		t.Fatalf("the revocation did not replace the content key: %+v", outcome)
	}
	if len(outcome.Repair) != 1 || !strings.HasPrefix(outcome.Repair[0], "dev_kept") {
		t.Fatalf("the browsers that must pair again are %v, wanted dev_kept alone", outcome.Repair)
	}
	stored, found, err := link.keys.MasterSecret()
	if err != nil || !found {
		t.Fatalf("the content key is unreadable after the revocation: %v %v", found, err)
	}
	if bytes.Equal(stored.Bytes(), before) {
		t.Fatal("the content key the revoked browser holds is still this machine's content key")
	}
	if _, handed, err := link.pairingKeys(); err != nil || !bytes.Equal(handed.Bytes(), stored.Bytes()) {
		t.Fatalf("the next pairing would not hand out the new key: %v", err)
	}
	// No socket was up, so nothing was taken down; the next one must not be
	// built from what the old wire() captured.
	link.mu.Lock()
	pending := link.rotated
	link.mu.Unlock()
	if outcome.Reconnected || !pending {
		t.Fatalf("the rebuild was not asked for: reconnected=%v pending=%v", outcome.Reconnected, pending)
	}
	if err := link.wire(); err != nil { // what Run does before its next socket
		t.Fatal(err)
	}
	if !bytes.Equal(link.relay.Secret.Bytes(), stored.Bytes()) {
		t.Fatal("the rebuilt line still seals with the replaced key")
	}
	// The browser that was kept holds the old key until it pairs again: its
	// envelopes no longer open, and once it holds the new key they do.
	if link.transport.AcceptDirect("dev_kept", kept.seal(t, 1, domaincloud.ContentKey{}, before)) {
		t.Fatal("an envelope sealed under the replaced key was opened")
	}
	if !link.transport.AcceptDirect("dev_kept", kept.seal(t, 2, stored, nil)) {
		t.Fatal("an envelope sealed under the new key was refused")
	}
	if link.transport.AcceptDirect("dev_revoked", revoked.seal(t, 1, stored, nil)) {
		t.Fatal("the revoked browser was admitted")
	}
	if dropped := link.transport.Status().InboundDropped[adaptercloud.DropDecryptFailed]; dropped != 1 {
		t.Fatalf("decrypt failures counted %d, wanted the one old-key envelope", dropped)
	}
	// Revoking a device that has no live pin changes nothing.
	again, err := link.Pairing().Revoke("dev_revoked")
	if err != nil || again.Revoked || again.SecretReplaced {
		t.Fatalf("revoking twice answered %+v %v", again, err)
	}
	if now, _, _ := link.keys.MasterSecret(); !bytes.Equal(now.Bytes(), stored.Bytes()) {
		t.Fatal("a revocation that changed nothing replaced the key")
	}
	if !slices.Equal(link.RotationCost(), outcome.Repair) {
		t.Fatalf("the repair list %v is not what is still paired: %v", outcome.Repair, link.RotationCost())
	}
}

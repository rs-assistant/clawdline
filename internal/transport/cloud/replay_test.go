package cloud

import (
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// An envelope this machine has answered is refused when it is handed over
// again — after a key rotation rebuilds the line, and after the daemon
// restarts. Before 2026-10-22 the window was in memory and every wire() built
// an empty one, so a relay holding a viewer's envelope could have it answered
// again by waiting for a reconnect.
func TestAnAnsweredEnvelopeStaysAnsweredAcrossARebuildAndARestart(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{"cloud_enabled":true}`)
	writeIdentity(t, dir, "")
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil || link.transport == nil {
		t.Fatalf("open: %v %s", err, link.Status().LastError)
	}
	viewer := pairTestViewer(t, link, "dev_viewer")
	secret, _, err := link.keys.MasterSecret()
	if err != nil {
		t.Fatal(err)
	}
	first := viewer.sealAt(t, 7, secret, time.Now())
	if !link.transport.AcceptDirect("dev_viewer", first) {
		t.Fatal("a fresh envelope from a paired viewer was refused")
	}
	if link.transport.AcceptDirect("dev_viewer", first) {
		t.Fatal("the same envelope was accepted twice on one line")
	}

	// A rotation rebuilds everything downstream of the keys.
	if err := link.wire(); err != nil {
		t.Fatal(err)
	}
	if link.transport.AcceptDirect("dev_viewer", first) {
		t.Fatal("a rebuilt line accepted an envelope the line before it had answered")
	}

	// A restart is a new link on the same directory.
	restarted, err := Open(LinkOptions{Dir: dir})
	if err != nil || restarted.transport == nil {
		t.Fatalf("reopen: %v %s", err, restarted.Status().LastError)
	}
	if restarted.transport.AcceptDirect("dev_viewer", first) {
		t.Fatal("a restarted daemon accepted an envelope it had answered before the restart")
	}
	if dropped := restarted.transport.Status().InboundDropped[adaptercloud.DropReplay]; dropped != 1 {
		t.Fatalf("the restarted line counted %d replays, wanted 1", dropped)
	}
	if !restarted.transport.AcceptDirect("dev_viewer", viewer.sealAt(t, 8, secret, time.Now())) {
		t.Fatal("the viewer's next envelope was refused after the restart")
	}
}

// A viewer's envelope is only as fresh as the ts it signed. One further than
// the relay's own window from this machine's clock is refused, whichever way
// it is off, before it spends a replay claim.
func TestAnEnvelopeOutsideTheClockWindowIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{"cloud_enabled":true}`)
	writeIdentity(t, dir, "")
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil || link.transport == nil {
		t.Fatalf("open: %v %s", err, link.Status().LastError)
	}
	viewer := pairTestViewer(t, link, "dev_viewer")
	secret, _, err := link.keys.MasterSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for seq, at := range map[uint64]time.Time{
		1: now.Add(-domaincloud.MaxTimestampSkew - time.Minute),
		2: now.Add(domaincloud.MaxTimestampSkew + time.Minute),
	} {
		if link.transport.AcceptDirect("dev_viewer", viewer.sealAt(t, seq, secret, at)) {
			t.Fatalf("an envelope stamped %s from now was accepted", at.Sub(now).Round(time.Minute))
		}
	}
	if dropped := link.transport.Status().InboundDropped[adaptercloud.DropClockSkew]; dropped != 2 {
		t.Fatalf("clock-skew drops %d, wanted 2", dropped)
	}
	// Inside the window either way is accepted, and the refused sequences
	// were never claimed.
	if !link.transport.AcceptDirect("dev_viewer", viewer.sealAt(t, 3, secret, now.Add(-4*time.Minute))) ||
		!link.transport.AcceptDirect("dev_viewer", viewer.sealAt(t, 1, secret, now.Add(4*time.Minute))) {
		t.Fatal("an envelope inside the clock window was refused")
	}
}

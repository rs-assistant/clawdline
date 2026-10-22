package cloudops

import (
	"context"
	"crypto/ed25519"
	"testing"
)

// A ctl/ read is re-authorized when it is answered, like a command, and minus
// only the write gate. Before 2026-10-22 a read went straight to its route on
// the transport's admission alone: a device revoked, unpaired or removed from
// the roster after its envelope was admitted still got the answer.
func TestAReadIsRefusedOnceItsSenderLosesAuthority(t *testing.T) {
	reads := []map[string]any{
		{"type": "places", "session": MachineReplySession, "request": "req-places"},
		{"type": "transcript", "session": pane, "limit": 100},
		{"type": "sessions.snapshot", "session": MachineReplySession, "request": "req-snap"},
	}
	denials := []struct {
		name   string
		say    Authority
		code   string
		status int
	}{
		{"revoked, unpaired or off the roster", Authority{ClockReady: true, RosterReadable: true}, "unknown_sender", 403},
		{"the roster could not be read", Authority{ClockReady: true, RosterAllowsSender: true}, "command_roster_unreadable", 503},
	}
	for _, read := range reads {
		for _, denial := range denials {
			t.Run(read["type"].(string)+"/"+denial.name, func(t *testing.T) {
				r := &router{}
				stated := 0
				b := Bridge{MachineID: "mac-01", Router: r,
					SessionsFor: func(context.Context, string, bool) (SessionsStated, *Refusal) {
						stated++
						return SessionsStated{Complete: true}, nil
					}}
				var asked []bool
				b.Authority = func(_ context.Context, sender string, _ ed25519.PublicKey, requiresWriteGate bool) Authority {
					if sender != "viewer-device-01" {
						t.Errorf("authority was asked about %q", sender)
					}
					asked = append(asked, requiresWriteGate)
					return denial.say
				}
				answer := b.Handle(context.Background(), request(t, ClassCtl, read))
				if answer.Code != denial.code || answer.Status != denial.status {
					t.Fatalf("answered %q/%d, wanted %q/%d", answer.Code, answer.Status, denial.code, denial.status)
				}
				if len(r.seen) != 0 || stated != 0 {
					t.Fatalf("a read from a sender without authority was answered: routes %+v, snapshots %d", r.seen, stated)
				}
				if len(asked) != 1 || asked[0] {
					t.Fatalf("a read asked for the write gate, or was not asked at all: %v", asked)
				}
				if !answer.Published() {
					t.Fatal("the refusal has nowhere to go, so the viewer would wait out its timeout")
				}
			})
		}
	}
	// With authority, the write gate off is still no reason to refuse a read.
	r := &router{}
	b := Bridge{MachineID: "mac-01", Router: r}
	b.Authority = func(_ context.Context, _ string, _ ed25519.PublicKey, requiresWriteGate bool) Authority {
		return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true, WriteGateAllows: !requiresWriteGate}
	}
	if answer := b.Handle(context.Background(), request(t, ClassCtl, reads[0])); !answer.OK() || len(r.seen) != 1 {
		t.Fatalf("a permitted read was refused: %+v %+v", answer, r.seen)
	}
}

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ttyHolder is iTerm2 with its listing switched off: it lists nothing, holds
// the ttys it is given, and records each action with whether it was asked for.
type ttyHolder struct {
	closeHost
	holds map[string]bool
	done  []string
}

func (h *ttyHolder) Name() string { return "iterm" }
func (h *ttyHolder) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Provenance: "iterm", Disabled: terminal.ITermScanDisabledBy}, nil
}
func (h *ttyHolder) HoldsTTY(_ context.Context, tty string) bool { return h.holds[tty] }
func (h *ttyHolder) note(ctx context.Context, what string, s session.Session) {
	asked := "polled"
	if ports.WasAsked(ctx) {
		asked = "asked"
	}
	h.done = append(h.done, what+" "+s.ID+" "+asked)
}
func (h *ttyHolder) Send(ctx context.Context, s session.Session, _ string) error {
	h.note(ctx, "send", s)
	return nil
}
func (h *ttyHolder) Interrupt(ctx context.Context, s session.Session) error {
	h.note(ctx, "interrupt", s)
	return nil
}

func ttyHolderActions(h *ttyHolder) Actions {
	return Actions{
		Inventory: Inventory{Process: looseScan{}, Terminals: []ports.TerminalHost{h}},
		Terminals: []ports.TerminalHost{h},
	}
}

// An assistant in a hand-opened iTerm2 tab, with iTerm2's listing switched
// off, is a row only the process table saw. A Note's reply to it answered
// backend_unsupported — "來源尚未確認" on the card — because such a row was
// refused before any backend was asked. The backend that holds its tty now
// carries the action out, and is told the action was asked for, which is what
// lets the switched-off iTerm2 send the Apple Events this one action needs.
func TestARowOnlyTheProcessTableSawIsDrivenByTheBackendHoldingItsTTY(t *testing.T) {
	ctx := context.Background()
	h := &ttyHolder{holds: map[string]bool{"ttys011": true}}
	a := ttyHolderActions(h)
	if _, err := a.Send(ctx, "ttys011", "option A"); err != nil {
		t.Fatalf("send = %v", err)
	}
	if _, err := a.Interrupt(ctx, "ttys011"); err != nil {
		t.Fatalf("interrupt = %v", err)
	}
	want := []string{"send ttys011 asked", "interrupt ttys011 asked"}
	if len(h.done) != len(want) || h.done[0] != want[0] || h.done[1] != want[1] {
		t.Fatalf("the backend did %v, want %v", h.done, want)
	}
}

// A tty no backend holds — a claude in Terminal.app, or in a tmux server on a
// socket of its own — is still refused before a byte is typed, and refused as
// having no backend rather than handed to whichever backend comes first.
func TestARowOnATTYNoBackendHoldsIsStillRefused(t *testing.T) {
	h := &ttyHolder{holds: map[string]bool{"ttys099": true}}
	a := ttyHolderActions(h)
	_, err := a.Send(context.Background(), "ttys011", "option A")
	var ref Refusal
	var unsent terminal.Unsent
	if !errors.As(err, &ref) || ref.Code != "backend_unsupported" || !errors.As(err, &unsent) {
		t.Fatalf("send = %v, want backend_unsupported before the first byte", err)
	}
	if len(h.done) != 0 {
		t.Fatalf("a backend that does not hold the tty was asked to act: %v", h.done)
	}
}

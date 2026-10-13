package app

import (
	"context"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// askingIdentity is an identity source whose transcript ends in an
// AskUserQuestion call, or does not.
type askingIdentity struct {
	asked []session.AskedQuestion
	open  bool
}

func (i askingIdentity) ForSession(context.Context, session.Session) (ports.Identity, bool) {
	return ports.Identity{}, false
}

func (i askingIdentity) OpenQuestions(context.Context, session.Session) ([]session.AskedQuestion, bool) {
	return i.asked, i.open
}

// The call behind menu-ask-live.txt, as its transcript records it.
var drinkAsked = []session.AskedQuestion{{
	Text: "What would you like this afternoon?",
	Options: []session.AskedOption{
		{Label: "Tea", Note: "A warm, soothing beverage with various flavor options."},
		{Label: "Water", Note: "Pure hydration, refreshing and essential."},
		{Label: "Coffee", Note: "A bold, energizing drink to boost your afternoon."},
	},
}}

func waitingClaude() session.Session {
	return session.Session{ID: "w0t0p0", Backend: session.BackendITerm, Assistant: session.AssistantClaude,
		ConversationID: "conv", State: session.StateWaiting, Evidence: session.EvidenceRegistry}
}

// iTerm2 answering no Apple Event (2026-10-02: four tabs opened at once,
// `osascript failed: signal: killed`) left a waiting row with no screen, and
// so with no buttons, though its transcript said exactly what was asked. The
// transcript now puts the menu together, numbered as Claude Code numbers it,
// and says where it came from.
func TestAnUnreadableScreenTakesTheMenuFromTheTranscript(t *testing.T) {
	row := waitingClaude()
	term := &menuTerminal{s: row, blind: true}
	in := Inventory{Screen: term, Identity: askingIdentity{asked: drinkAsked, open: true}}

	got := in.readScreen(context.Background(), row)
	if got.Menu == nil {
		t.Fatal("no menu, though the transcript names the open question")
	}
	m := got.Menu
	want := []struct {
		n     int
		label string
	}{{1, "Tea"}, {2, "Water"}, {3, "Coffee"}, {4, "Type something."}, {5, "Chat about this"}}
	if len(m.Options) != len(want) {
		t.Fatalf("rows %+v", m.Options)
	}
	for i, w := range want {
		if m.Options[i].Number != w.n || m.Options[i].Label != w.label {
			t.Fatalf("row %d is %d %q, want %d %q", i, m.Options[i].Number, m.Options[i].Label, w.n, w.label)
		}
	}
	if m.Source != session.MenuSourceTranscript || m.Selected != nil || m.Question != drinkAsked[0].Text {
		t.Fatalf("menu %+v", m)
	}
	if got.Screen != session.ScreenUnavailable || got.State != session.StateWaiting || got.Line != session.MenuRevision(*m) {
		t.Fatalf("row screen %q state %q line %q", got.Screen, got.State, got.Line)
	}

	// A registry that does not say waiting is not a question being asked,
	// whatever the transcript's last entry is.
	idle := row
	idle.State = session.StateIdle
	if got := in.readScreen(context.Background(), idle); got.Menu != nil {
		t.Fatalf("an idle row got %+v", got.Menu)
	}
}

// With a screen, the screen decides: its caret, its numbers, its source.
func TestAReadableScreenStillDecidesTheMenu(t *testing.T) {
	b, err := os.ReadFile("../domain/session/testdata/menu-ask-live.txt")
	if err != nil {
		t.Fatal(err)
	}
	row := waitingClaude()
	term := &menuTerminal{s: row, screen: string(b)}
	in := Inventory{Screen: term, Identity: askingIdentity{asked: drinkAsked, open: true}}
	got := in.readScreen(context.Background(), row)
	if got.Menu == nil || got.Menu.Source != "" || got.Menu.Selected == nil || *got.Menu.Selected != 1 {
		t.Fatalf("menu %+v", got.Menu)
	}
	if got.Screen != session.ScreenRead {
		t.Fatalf("screen %q, want read", got.Screen)
	}
}

// A Codex command approval is positive screen evidence that the session is
// waiting, even before any assistant registry has a state for the tmux pane.
// This is the path the fleet list uses for a dispatched Linux session.
func TestCodexCommandApprovalMakesAnUnknownTmuxSessionWaiting(t *testing.T) {
	b, err := os.ReadFile("../domain/session/testdata/menu-codex-command-approval.txt")
	if err != nil {
		t.Fatal(err)
	}
	row := session.Session{ID: "%62", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		State: session.StateUnknown, Evidence: session.EvidenceProcess}
	term := &menuTerminal{s: row, screen: string(b)}
	got := (Inventory{Screen: term}).readScreen(context.Background(), row)
	if got.State != session.StateWaiting || got.Evidence != session.EvidenceScreen || got.Menu == nil {
		t.Fatalf("row state %q evidence %q menu %+v", got.State, got.Evidence, got.Menu)
	}
	if got.Menu.Selected == nil || *got.Menu.Selected != 1 || got.Line != session.MenuRevision(*got.Menu) {
		t.Fatalf("menu %+v line %q", got.Menu, got.Line)
	}
}

// A transcript whose last entry is not an unanswered AskUserQuestion has
// nothing to say about what is drawn, so the row stays without buttons — and
// says it had no screen, which is a different thing from a screen with a
// dialog nothing recognised.
func TestNoOpenQuestionNoTranscriptMenu(t *testing.T) {
	row := waitingClaude()
	term := &menuTerminal{s: row, blind: true}
	in := Inventory{Screen: term, Identity: askingIdentity{asked: drinkAsked, open: false}}
	got := in.readScreen(context.Background(), row)
	if got.Menu != nil {
		t.Fatalf("made %+v from a transcript with no open question", got.Menu)
	}
	if got.Screen != session.ScreenUnavailable {
		t.Fatalf("screen %q, want unavailable", got.Screen)
	}
}

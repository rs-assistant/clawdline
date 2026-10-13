// Package tmuxview opens a session the way projects.PlanITermTmux says: the
// program in a new detached tmux session, and an iTerm2 tab that only shows it.
//
// iTerm2's Apple Events stall in episodes of minutes (eleven between
// 2026-09-23 and 2026-10-02 in one daemon's log): a screen read is killed at
// its limit, a send answers "iTerm2 did not answer in time.", and a session
// that lives in an iTerm2 tab can be neither read nor typed into until it
// passes. A tmux capture of the same screen took about 4 ms with no lock and
// no Apple Event. So the session this opens is the tmux pane — every reading,
// line, key, interrupt and close goes through tmux — and the one Apple Event
// left is the one that opens the tab
// (docs/interface.md, "An iTerm2 tab that only shows a tmux session").
//
// One helper for every caller that opens a session — the start route, a squad
// launch, a child, a hand-over — so the four cannot drift apart on what a
// viewer is.
package tmuxview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// Backend is the session backend a viewed session is recorded under.
const Backend = "tmux"

// The optional viewer runs after the pane has been returned. One attempt at a
// time prevents stalled Apple Events from accumulating when starts arrive in a
// burst; a busy viewer lane is skipped because the browser already has the pane.
const viewerOpenLimit = 20 * time.Second
const viewerSlotLimit = 1

var viewerSlot = make(chan struct{}, viewerSlotLimit)

// ViewerOpenCount is the number of optional iTerm2 viewer attempts in flight.
func ViewerOpenCount() int { return len(viewerSlot) }

// Opened is a session started in tmux.
type Opened struct {
	// PaneID is the tmux pane the program runs in: the session's id.
	PaneID string
	// The viewer finishes independently of the start reply. These fields stay
	// empty for callers that still consume the common start result.
	TabID  string
	Attach string
}

// Open starts command in a new detached tmux session called name, in cwd, on
// the person's default tmux server. It then attempts to open an iTerm2 viewer
// in the background. The browser can read and type into the pane immediately.
//
// A viewer failure is logged; it does not turn a running browser Session into
// a detached start. Only a failed tmux launch fails the start reply.
func Open(ctx context.Context, l ports.Launcher, cwd, name, command string) (Opened, error) {
	pane, err := l.NewTmuxSession(ctx, cwd, name, command)
	if err != nil {
		return Opened{}, err
	}
	select {
	case viewerSlot <- struct{}{}:
		go func() {
			defer func() { <-viewerSlot }()
			viewerCtx, cancel := context.WithTimeout(context.Background(), viewerOpenLimit)
			defer cancel()
			line, err := l.PrepareTmuxViewer(viewerCtx, name)
			if err == nil {
				_, err = l.NewITermTab(viewerCtx, line)
			}
			if err != nil {
				log.Printf("tmuxview: tmux session %s (pane %s) started, but no iTerm2 tab shows it: %v", name, pane, err)
			}
		}()
	default:
		log.Printf("tmuxview: tmux session %s (pane %s) started; iTerm2 viewer busy", name, pane)
	}
	return Opened{PaneID: pane}, nil
}

// SessionName is a new tmux session name for a session nobody else names:
// `clawdline-<kind>-` and eight random hex. A name tmux already has is refused
// by `new-session`, never reused, so a collision is a failed open, not a
// session shared with somebody else's.
func SessionName(kind string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on the platforms this builds for; if it
		// did, the fixed name is still refused by tmux rather than shared.
		return "clawdline-" + kind
	}
	return "clawdline-" + kind + "-" + hex.EncodeToString(b[:])
}

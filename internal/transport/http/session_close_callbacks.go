package http

import (
	"context"
	"log"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// stopHeavyCallbacksReason is the verdict a closed Session's heavy callback
// settles with, read by `task show` and the task list.
const stopHeavyCallbacksReason = "its root Session was closed, so nobody was left to read the result"

// stopHeavyCallbacks cancels the heavy callbacks the closed Session handed to
// the daemon. A queued `clawdline heavy --handoff` used to hold the Session
// open as a live descendant, and when it was forced closed anyway the command
// still waited for the compile slot and ran for a composer that was gone. A
// wait callback is left alone: it may be a deploy, and stopping one halfway is
// worse than a notice nobody reads.
func (s *Server) stopHeavyCallbacks(ctx context.Context, closed session.Session, actor string) {
	if s.broker == nil {
		return
	}
	records, err := s.broker.LiveRecords(ctx)
	if err != nil {
		log.Printf("session close: heavy callbacks of %s were not read: %v", closed.ID, err)
		return
	}
	for _, id := range heavyCallbacksOf(records, closed, s.reading(ctx).Sessions) {
		_, err := s.broker.CancelTask(ctx, id, stopHeavyCallbacksReason, "",
			orchestrator.CancelCaller{Person: true, Principal: actor})
		if err != nil {
			log.Printf("session close: heavy callback %s of %s was not stopped: %v", id, closed.ID, err)
		}
	}
}

// heavyCallbacksOf is the live heavy callbacks whose root is the closed
// Session's conversation. None while another terminal still holds that
// conversation: the root is still there, and it is still waiting.
func heavyCallbacksOf(records []orchestrator.Record, closed session.Session, sessions []session.Session) []string {
	if closed.ConversationID == "" || !closed.IsAssistant() {
		return nil
	}
	for _, other := range sessions {
		if other.ID != closed.ID && other.Assistant == closed.Assistant && other.ConversationID == closed.ConversationID {
			return nil
		}
	}
	var out []string
	for _, r := range records {
		if r.Callback == nil || r.Callback.Intent != orchestrator.CallbackIntentHeavy || r.State.Terminal() ||
			r.Root == nil || r.Root.SessionID != closed.ConversationID ||
			(r.Root.Assistant != "" && r.Root.Assistant != string(closed.Assistant)) {
			continue
		}
		out = append(out, r.ID)
	}
	return out
}

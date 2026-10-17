package orchestrator

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// What a scheduled run's child keeps of its conversation, and why its own
// timeout does not end it while it is still working.
//
// A scheduled run has nobody waiting on its tab (D07), so what ends it is the
// child's own result.json or, failing that, its timeout. Until 2026-10-15 the
// timeout ended it on the wall clock alone, counted from the moment the
// occurrence was queued: a run that waited seventeen minutes for the machine
// had thirteen left, and a child told "you have 30 minutes" wrapped up at the
// thirtieth whatever it was in the middle of, after which on_success closed
// its tab. The person's rule is that a scheduled Session ends when it reports
// a delivery or a failure, and not because a clock ran out mid-task.

// noteChildConversation records the conversation the child's tab holds the
// first time a reading binds one. Scheduled runs need it for resume; every
// task needs it so a finished task cannot rename a later conversation in a
// reused terminal.
func (b *Broker) noteChildConversation(ctx context.Context, rd reading, r Record) Record {
	if r.ChildTerminalID == "" || r.ChildConversationID != "" || r.State.Terminal() {
		return r
	}
	s, ok := rd.session(r.ChildTerminalID)
	if !ok || !s.IsAssistant() || s.ConversationID == "" {
		return r
	}
	if s.Assistant != "" && string(s.Assistant) != r.Assistant {
		return r
	}
	id := s.ConversationID
	updated, err := b.mutate(ctx, r.ID, "task.child.conversation", func(t *Record) error {
		if t.ChildConversationID != "" || t.State.Terminal() || t.ChildTerminalID != r.ChildTerminalID {
			return errUnchanged
		}
		t.ChildConversationID = id
		return nil
	})
	if err != nil || updated.ID == "" {
		return r
	}
	return updated
}

// scheduledRunStillWorking is whether a scheduled run past its timeout is
// still at work in its tab, and so is not ended by that timeout. A tab this
// reading did not see, or one at rest at its prompt, is not: such a run never
// wrote its result, and is ended as before so the schedule's next occurrence
// is not held behind it forever. A tab holding a question for the person is
// still the run's, and waits for them.
func scheduledRunStillWorking(rd reading, r Record) bool {
	if r.ScheduleID == "" || r.Root != nil || r.ChildTerminalID == "" {
		return false
	}
	s, ok := rd.session(r.ChildTerminalID)
	if !ok || !s.IsAssistant() {
		return false
	}
	return s.State == session.StateWorking || s.State == session.StateWaiting
}

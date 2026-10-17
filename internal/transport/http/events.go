package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
)

// events serves the console's one stream, which this daemon publishes itself
// (see ownEvents).
func (s *Server) events(w http.ResponseWriter, r *http.Request) { s.ownEvents(w, r) }

// The broker's progress notes, on the stream (docs/broker-design.md E4, #25).
//
// A child's note is never typed into its root's terminal: that would interrupt
// a root in the middle of the turn it is waiting in. It used to reach nobody
// at all — two shipped guides said a note would wake the root, and a child
// once asked its root a question that way, heard nothing, and decided alone.
// So a note goes where the root's person is looking instead: this stream, as
// an `orchestrator-progress` event. A child that must wake somebody has
// /notify.
//
// Each note's identity is its row in the store. A connection is sent the
// newest notes of every live task when it opens — a page that reconnects is
// level without asking, and never has to replay anything — and then each note
// as it is accepted, and never the same note twice.

// progressFeed is one connection's view of the broker's notes.
type progressFeed struct {
	notes  <-chan store.BrokerNote
	cancel func()
	// sent is the identities already written, bounded: notes are published
	// after their commit, so two notes can arrive out of order, and a
	// high-water mark would drop the earlier one.
	sent  map[int64]bool
	order []int64
}

const progressIdentitiesKept = 1024

// openProgress subscribes before anything is read, so a note accepted while
// the snapshot is being built is either in the snapshot or on the channel —
// possibly both, which the identity check absorbs — and never in neither.
func (s *Server) openProgress() *progressFeed {
	f := &progressFeed{sent: map[int64]bool{}}
	if s.broker == nil {
		ch := make(chan store.BrokerNote)
		f.notes, f.cancel = ch, func() {}
		return f
	}
	f.notes, f.cancel = s.broker.SubscribeProgress()
	return f
}

func (f *progressFeed) close() { f.cancel() }

// snapshot writes the newest notes of every live task, as one frame, when
// there are any.
func (f *progressFeed) snapshot(ctx context.Context, s *Server, w http.ResponseWriter, flusher http.Flusher) {
	if s.broker == nil {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	notes, err := s.broker.RecentProgress(readCtx)
	if err != nil || len(notes) == 0 {
		return
	}
	f.emit(w, flusher, notes)
}

// write sends one note, unless this connection has already been sent it.
func (f *progressFeed) write(w http.ResponseWriter, flusher http.Flusher, n store.BrokerNote) {
	f.emit(w, flusher, []store.BrokerNote{n})
}

func (f *progressFeed) emit(w http.ResponseWriter, flusher http.Flusher, notes []store.BrokerNote) {
	frame := contract.BrokerProgressFrame{Notes: []contract.BrokerProgressEvent{}}
	for _, n := range notes {
		if f.sent[n.Seq] {
			continue
		}
		f.remember(n.Seq)
		frame.Notes = append(frame.Notes, contract.BrokerProgressEvent{
			Seq: n.Seq, TaskID: n.TaskID, Note: n.Note, At: n.At.Unix(),
		})
	}
	if len(frame.Notes) == 0 {
		return
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return
	}
	// No `id:` line. The stream's ids are the session scan's generation, and
	// a second numbering on the same stream would make Last-Event-ID mean two
	// things; the snapshot on reconnect is what makes a page level instead.
	fmt.Fprintf(w, "event: orchestrator-progress\ndata: %s\n\n", payload)
	flusher.Flush()
}

func (f *progressFeed) remember(seq int64) {
	f.sent[seq] = true
	f.order = append(f.order, seq)
	if len(f.order) > progressIdentitiesKept {
		delete(f.sent, f.order[0])
		f.order = f.order[1:]
	}
}

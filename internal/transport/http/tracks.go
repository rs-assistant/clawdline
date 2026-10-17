package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// `GET /v1/board/tracks?project=&track=&cursor=`: the old app's cards placed
// on the three tracks of docs/board-redesign.md (board / session to-do / backlog),
// read-only (§9 step 1, design-decisions T1).
//
// The cards and their logs were the retired Swift app's, and neither is read
// any more (docs/cutover.md §7): both sources answer `disabled`, the word the
// contract has for "not read, and known to hold nothing", and the projection
// is over no cards. It writes nothing and has no command.

// TracksPageSize is the fixed page: no byte budget and no size parameter
// (design-decisions D34).
const TracksPageSize = 200

type tracksDeps struct {
	presence func(context.Context) (work.Presence, work.PresenceSource)
	now      func() time.Time
}

var tracksByServer sync.Map // *Server -> *tracksDeps

func (s *Server) tracks() *tracksDeps {
	if d, ok := tracksByServer.Load(s); ok {
		return d.(*tracksDeps)
	}
	d, _ := tracksByServer.LoadOrStore(s, &tracksDeps{
		presence: s.tracksPresence,
		now:      time.Now,
	})
	return d.(*tracksDeps)
}

// tracksPresence is this daemon's reading of the running assistants.
//
// A card's spans name a session by its terminal id or by its conversation id,
// so both are collected. The reading counts as complete only when every source
// answered and every assistant in it has a conversation id: an assistant whose
// conversation could not be identified may be the very session a span names,
// and it must not be read as gone.
func (s *Server) tracksPresence(ctx context.Context) (work.Presence, work.PresenceSource) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	inv := s.reading(ctx)
	p := work.Presence{Complete: inv.Complete, Sessions: map[string]bool{}}
	count := 0
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		count++
		p.Sessions[strings.ToLower(item.ID)] = true
		if item.ConversationID == "" {
			p.Complete = false
			continue
		}
		p.Sessions[strings.ToLower(item.ConversationID)] = true
	}
	return p, work.PresenceSource{From: "inventory", Complete: p.Complete, Sessions: count}
}

var tracksQueryKeys = map[string]bool{"project": true, "track": true, "cursor": true}

func (s *Server) boardTracks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The tracks are read with GET.")
		return
	}
	values := r.URL.Query()
	for key, v := range values {
		if !tracksQueryKeys[key] || len(v) != 1 || v[0] == "" || len(v[0]) > 200 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Unknown board tracks query field.")
			return
		}
	}
	track := work.Track(values.Get("track"))
	if track != "" && track != work.TrackTodo && track != work.TrackBoard && track != work.TrackBacklog {
		writeRefusal(w, http.StatusBadRequest, "invalid_track", "A track is todo, board or backlog.")
		return
	}

	d := s.tracks()
	project := values.Get("project")
	if project != "" {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "This board has no such project.")
		return
	}
	board := work.BoardSource{Status: "disabled"}
	history := work.HistorySource{Status: "disabled"}
	presence, presenceSource := d.presence(r.Context())

	projection := work.Project(nil, presence, d.now(), work.DefaultStall)
	rows, next, err := projection.Page(track, values.Get("cursor"), TracksPageSize)
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "invalid_cursor", "That cursor was not written by this route.")
		return
	}
	projection.Rows = rows
	body := work.Tracks{Projection: projection, PageSize: TracksPageSize,
		Sources: work.Sources{Board: board, History: history, Presence: presenceSource}}
	if next != "" {
		body.NextCursor = &next
	}
	out, err := json.Marshal(map[string]any{"tracks": body})
	if err != nil {
		writeRawRefusal(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

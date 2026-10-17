package http

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

const callbackShellPrefix = "callback-"

func callbackShell(record orchestrator.Record, places map[string]heavySlotPlace) contract.SessionShell {
	cb := record.Callback
	phase := "queued"
	if !cb.StartedAt.IsZero() {
		phase = "running"
	}
	if p, ok := places[record.ID]; ok {
		phase = p.phase
	}
	if record.State.Terminal() {
		phase = string(record.State)
	}
	row := contract.SessionShell{ID: callbackShellPrefix + record.ID, Kind: "callback", What: record.Title, Command: orchestrator.CallbackCommand(record), Directory: cb.Dir, At: record.CreatedAt.Unix(), State: phase}
	if !cb.StartedAt.IsZero() {
		row.StartedAt = cb.StartedAt.Unix()
	}
	return row
}

func (s *Server) callbackShells(ctx context.Context, places map[string]heavySlotPlace) (map[string][]contract.SessionShell, bool) {
	out := map[string][]contract.SessionShell{}
	if s.broker == nil {
		return out, true
	}
	records, bad, err := s.broker.Records(ctx)
	if err != nil || len(bad) > 0 {
		return nil, false
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	for _, r := range records {
		if r.Kind != orchestrator.TaskKindCallback || r.Callback == nil || r.Root == nil || r.State.Terminal() {
			continue
		}
		out[r.Root.SessionID] = append(out[r.Root.SessionID], callbackShell(r, places))
	}
	return out, true
}

func (s *Server) sessionCallbackOutput(w http.ResponseWriter, r *http.Request, sessionID, shellID string) {
	if s.broker == nil {
		writeRefusal(w, http.StatusNotFound, "not_found", "This daemon has no background callback.")
		return
	}
	item, err := s.actions().Find(r.Context(), sessionID)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	record, out, err := s.broker.ReadCallbackOutput(r.Context(), strings.TrimPrefix(shellID, callbackShellPrefix), item.ConversationID)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	_, _, places := s.runningHeavyCallback(r.Context())
	writeJSON(w, contract.ShellOutputReply{Shell: callbackShell(record, places), Text: out.Text, Truncated: out.Truncated, Ended: record.State.Terminal(), Signature: out.Signature})
}

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// squadRoleDraft spends one explicit, tool-less assistant turn and returns
// editable text. It never writes to the catalog.
func (s *Server) squadRoleDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A role draft is requested with POST.")
		return
	}
	key, ok := squadWriteDoor(w, r)
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, CapacityLimit(capacity.IntentRequestBytes)+128))
	if err != nil || len(raw) > int(CapacityLimit(capacity.IntentRequestBytes))+127 {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "The role description is too long.")
		return
	}
	var request struct {
		Description string `json:"description"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&request) != nil || dec.Decode(new(any)) != io.EOF ||
		strings.TrimSpace(request.Description) == "" || len(request.Description) > int(CapacityLimit(capacity.IntentRequestBytes)) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Describe the role in one bounded text field.")
		return
	}
	provider := boardViewer(r).NarrativeProvider
	receipt := store.ReceiptKey{Scope: "squad-role-draft", Actor: accessOf(r).verdict.Device, Key: key}
	s.receipted(w, r, receipt, requestDigest(raw), func(status int) bool {
		return status != http.StatusTooManyRequests && status != http.StatusNotImplemented
	}, func(w http.ResponseWriter) {
		if !s.enterIntent() {
			writeRefusal(w, http.StatusTooManyRequests, "busy", "This machine is already using both small-turn slots.")
			return
		}
		defer s.leaveIntent()
		s.intentRun.Lock()
		defer s.intentRun.Unlock()
		turn, cancel := context.WithTimeout(r.Context(), time.Duration(CapacityLimit(capacity.IntentPlannerSeconds))*time.Second)
		defer cancel()
		drafter := s.draftRole
		if drafter == nil {
			p := planner.New()
			p.Timeout = time.Duration(CapacityLimit(capacity.IntentPlannerSeconds)) * time.Second
			drafter = p.DraftRole
		}
		started := time.Now()
		draft, err := drafter(turn, request.Description, provider)
		if err != nil {
			log.Printf("audit squad.role_draft provider=%s ms=%d ok=0", provider, time.Since(started).Milliseconds())
			switch {
			case errors.Is(err, planner.ErrNoPlanner):
				writeRefusal(w, http.StatusNotImplemented, "no_role_drafter", "The configured AI provider is not installed on this machine.")
			case errors.Is(err, planner.ErrOutOfQuota):
				writeRefusal(w, http.StatusServiceUnavailable, "role_drafter_out_of_quota", "The configured AI provider has no usage left.")
			default:
				writeRefusal(w, http.StatusBadGateway, "role_draft_failed", "The AI provider did not return a usable role draft.")
			}
			return
		}
		log.Printf("audit squad.role_draft provider=%s ms=%d ok=1", provider, time.Since(started).Milliseconds())
		writeJSON(w, draft)
	})
}

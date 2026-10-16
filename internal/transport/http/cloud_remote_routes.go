package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/session"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type remoteTaskLine interface {
	PublishTaskStart(context.Context, agenthandoff.TaskStart, []byte) (cloudtransport.TaskStartSubmission, error)
	RetryTaskStart(context.Context, string) (cloudtransport.TaskStartSubmission, error)
	PublishTaskEvent(context.Context, agenthandoff.TaskEvent, []byte) error
	RetryTaskEvent(context.Context, string) error
}

func (s *Server) remoteLine() remoteTaskLine {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		return nil
	}
	remote, _ := line.(remoteTaskLine)
	return remote
}

// currentRemoteSession resolves a CLI conversation or terminal ID to exactly
// one fresh local assistant process and its kernel-pinned generation.
func (s *Server) currentRemoteSession(ctx context.Context, id string) (agenthandoff.Endpoint, error) {
	if id == "" || s.executionMachine() == "" {
		return agenthandoff.Endpoint{}, errors.New("remote_source_required")
	}
	inv := s.freshReading(ctx)
	items := make([]session.Session, 0, len(inv.Sessions))
	lives := make([]swiftstore.Live, 0, len(inv.Sessions))
	var found session.Session
	count := 0
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		items = append(items, item)
		lives = append(lives, liveOf(item))
		if item.ID == id || item.ConversationID != "" && item.ConversationID == id {
			found = item
			count++
		}
	}
	if count != 1 || !currentExecution(found, inv) {
		return agenthandoff.Endpoint{}, errors.New("remote_source_unverified")
	}
	generation := s.observeExecutions(ctx, inv, items, lives)[found.ID]
	if generation == "" {
		return agenthandoff.Endpoint{}, errors.New("remote_source_unverified")
	}
	return agenthandoff.Endpoint{MachineID: s.executionMachine(), SessionID: found.ID,
		ExecutionGeneration: generation}, nil
}

func (s *Server) remoteMachinesRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET")
		return
	}
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", "The paired machine list is unavailable.")
		return
	}
	defer st.Close()
	pairs, err := st.ListPairs(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", "The paired machine list is unavailable.")
		return
	}
	type machine struct {
		MachineID string    `json:"machine_id"`
		PairID    string    `json:"pair_id"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	items := []machine{}
	for _, pair := range pairs {
		if pair.SourceMachineID == s.executionMachine() && pair.TargetSignature != "" &&
			pair.RevokedAt.IsZero() && time.Now().Before(pair.ExpiresAt) {
			items = append(items, machine{MachineID: pair.TargetMachineID, PairID: pair.ID,
				ExpiresAt: pair.ExpiresAt})
		}
	}
	writeJSON(w, map[string]any{"machines": items, "presence": "unknown"})
}

func (s *Server) remoteTasksRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST")
		return
	}
	line := s.remoteLine()
	if line == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_unavailable", "The remote task line is unavailable.")
		return
	}
	var input struct {
		TaskID        string `json:"task_id"`
		RequestID     string `json:"request_id"`
		SourceSession string `json:"source_session"`
		MachineID     string `json:"machine_id"`
		ProjectID     string `json:"project_id"`
		PairID        string `json:"pair_id"`
		Assistant     string `json:"assistant"`
		Title         string `json:"title"`
		Instructions  string `json:"instructions"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, agenthandoff.MaxBodyBytes+4096)).Decode(&input) != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The remote task request is invalid.")
		return
	}
	source, err := s.currentRemoteSession(r.Context(), input.SourceSession)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_source_unverified", "The source Session is not current.")
		return
	}
	body, err := json.Marshal(agenthandoff.TaskBody{Assistant: input.Assistant,
		Title: input.Title, Instructions: input.Instructions})
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The remote task body is invalid.")
		return
	}
	sum := sha256.Sum256(body)
	start := agenthandoff.TaskStart{TaskID: input.TaskID, RequestID: input.RequestID,
		Source: source, TargetMachineID: input.MachineID, TargetProjectID: input.ProjectID,
		PairID: input.PairID, BodyDigest: hex.EncodeToString(sum[:])}
	if agenthandoff.ValidateTaskStart(start, body) != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The remote task fields are invalid.")
		return
	}
	answer, err := line.PublishTaskStart(r.Context(), start, body)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_dispatch_refused", err.Error())
		return
	}
	writeJSON(w, answer)
}

func (s *Server) remoteTaskRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/remote/tasks/"), "/")
	if len(parts) == 0 || !orchestrator.IsTaskID(parts[0]) || len(parts) > 2 {
		writeRefusal(w, http.StatusBadRequest, "bad_task_id", "A remote task ID is required.")
		return
	}
	id := parts[0]
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", "The remote task is unavailable.")
		return
	}
	defer st.Close()
	if r.Method == http.MethodGet && len(parts) == 1 {
		s.remoteTaskShow(w, r, st, id)
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 {
		switch parts[1] {
		case "finish", "messages":
			s.remoteTaskEffect(w, r, st, id, parts[1])
			return
		case "ack":
			s.remoteTaskAck(w, r, st, id)
			return
		case "retry":
			if line := s.remoteLine(); line != nil {
				if _, err := line.RetryTaskStart(r.Context(), id); err == nil {
					writeJSON(w, map[string]any{"task_id": id, "state": "resubmitted"})
					return
				}
			}
		}
	}
	writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The remote task action is unsupported.")
}

func (s *Server) remoteTaskShow(w http.ResponseWriter, r *http.Request, st *peerstore.Store, id string) {
	current, err := s.currentRemoteSession(r.Context(), r.URL.Query().Get("session_id"))
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_source_unverified", "The current Session cannot be verified.")
		return
	}
	if start, err := st.TaskStartOutbox(r.Context(), id); err == nil && start.Start.Source == current {
		events, err := st.TaskEvents(r.Context(), id)
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", "The task events are unavailable.")
			return
		}
		state := "submitted"
		if start.RelayDelivered == "delivered" {
			state = "relay_delivered"
		}
		if start.TargetSessionID != "" {
			state = "session_opened"
		}
		for i := range events {
			if events[i].Direction == "in" && events[i].Event.Kind == agenthandoff.TaskEventFinish {
				if err := st.MarkTaskFinishObserved(r.Context(), events[i].Event); err != nil {
					writeRefusal(w, http.StatusServiceUnavailable, "remote_observation_unavailable", err.Error())
					return
				}
				updated, err := st.TaskEvent(r.Context(), events[i].Event)
				if err != nil {
					writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", err.Error())
					return
				}
				events[i] = updated
				state = "source_observed"
				if !updated.AcknowledgedAt.IsZero() {
					state = "source_acknowledged"
				}
			}
		}
		start.Frame = nil
		for i := range events {
			events[i].Frame = nil
		}
		writeJSON(w, map[string]any{"role": "source", "state": state, "task": start, "events": events})
		return
	}
	if task, err := st.RemoteTask(r.Context(), id); err == nil &&
		task.TargetSessionID == current.SessionID && task.TargetGeneration == current.ExecutionGeneration &&
		task.Start.TargetMachineID == current.MachineID {
		events, err := st.TaskEvents(r.Context(), id)
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", "The task events are unavailable.")
			return
		}
		for i := range events {
			events[i].Frame = nil
		}
		writeJSON(w, map[string]any{"role": "target", "state": task.State, "task": task, "events": events})
		return
	}
	writeRefusal(w, http.StatusNotFound, "remote_task_missing", "No remote task belongs to this Session.")
}

func (s *Server) remoteTaskAck(w http.ResponseWriter, r *http.Request, st *peerstore.Store, id string) {
	var input struct {
		SessionID string `json:"session_id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&input) != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The acknowledgement is invalid.")
		return
	}
	current, err := s.currentRemoteSession(r.Context(), input.SessionID)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_source_unverified", err.Error())
		return
	}
	start, err := st.TaskStartOutbox(r.Context(), id)
	if err != nil || start.Start.Source != current {
		writeRefusal(w, http.StatusNotFound, "remote_task_missing", "No remote task belongs to this Session.")
		return
	}
	events, err := st.TaskEvents(r.Context(), id)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_store_unavailable", err.Error())
		return
	}
	for _, event := range events {
		if event.Direction == "in" && event.Event.Kind == agenthandoff.TaskEventFinish {
			if err := st.MarkTaskFinishAcknowledged(r.Context(), event.Event); err != nil {
				writeRefusal(w, http.StatusConflict, "remote_ack_refused", err.Error())
				return
			}
			writeJSON(w, map[string]any{"task_id": id, "state": "source_acknowledged"})
			return
		}
	}
	writeRefusal(w, http.StatusConflict, "remote_finish_missing", "The target has not finished this task.")
}

func (s *Server) remoteTaskEffect(w http.ResponseWriter, r *http.Request, st *peerstore.Store,
	id, action string) {
	var input struct {
		SessionID string `json:"session_id"`
		RequestID string `json:"request_id"`
		Text      string `json:"text"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, agenthandoff.MaxBodyBytes+1024)).Decode(&input) != nil ||
		input.Text == "" || len(input.Text) > agenthandoff.MaxBodyBytes {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The task message is invalid.")
		return
	}
	current, err := s.currentRemoteSession(r.Context(), input.SessionID)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "remote_source_unverified", "The current Session cannot be verified.")
		return
	}
	var event agenthandoff.TaskEvent
	finishingTarget := false
	if task, err := st.RemoteTask(r.Context(), id); err == nil &&
		task.TargetSessionID == current.SessionID && task.TargetGeneration == current.ExecutionGeneration &&
		task.Start.TargetMachineID == current.MachineID {
		if task.State != "opened" && !(action == "finish" && task.State == "finished" && task.Result == input.Text) {
			writeRefusal(w, http.StatusConflict, "remote_task_state_changed", "The target task is not open.")
			return
		}
		event = agenthandoff.TaskEvent{Start: task.Start, Sender: current,
			Recipient: task.Start.Source}
		if action == "finish" {
			event.Kind = agenthandoff.TaskEventFinish
			finishingTarget = true
		} else {
			event.Kind = agenthandoff.TaskEventMessage
		}
	} else if start, err := st.TaskStartOutbox(r.Context(), id); err == nil &&
		start.Start.Source == current && start.TargetSessionID != "" && action == "messages" {
		event = agenthandoff.TaskEvent{Start: start.Start, Sender: current,
			Recipient: agenthandoff.Endpoint{MachineID: start.Start.TargetMachineID,
				SessionID: start.TargetSessionID, ExecutionGeneration: start.TargetGeneration},
			Kind: agenthandoff.TaskEventMessage}
	} else {
		writeRefusal(w, http.StatusNotFound, "remote_task_missing", "No remote task belongs to this Session.")
		return
	}
	event.RequestID = input.RequestID
	sum := sha256.Sum256([]byte(input.Text))
	event.BodyDigest = hex.EncodeToString(sum[:])
	if agenthandoff.ValidateTaskEvent(event, []byte(input.Text)) != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The task event fields are invalid.")
		return
	}
	line := s.remoteLine()
	if line == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "remote_unavailable", "The remote task line is unavailable.")
		return
	}
	if finishingTarget {
		if err := st.MarkRemoteTaskFinished(r.Context(), id, input.Text); err != nil {
			writeRefusal(w, http.StatusConflict, "remote_finish_refused", err.Error())
			return
		}
	}
	if err := line.PublishTaskEvent(r.Context(), event, []byte(input.Text)); err != nil {
		writeRefusal(w, http.StatusConflict, "remote_event_refused", err.Error())
		return
	}
	writeJSON(w, map[string]any{"task_id": id, "request_id": event.RequestID,
		"relay_delivered": "unknown", "machine_execution": "unknown"})
}

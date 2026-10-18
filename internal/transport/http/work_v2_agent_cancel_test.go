package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestAgentCancellationRelaysThePersonsRunAndKeepsTheAudit(t *testing.T) {
	s, p, project := sessionItemServer(t)
	created := personItem(t, s, project, "feature", "cancel-created")
	assigned, err := s.assignWorkV2(context.Background(), created.ID, "local", created.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run := issueTestRun(t, s, p, "Please remove the Queued assignment Board item")
	path := "/v1/work/v2/agent/items/" + created.ID + "/cancel"
	body := `{"session_id":"` + p.s.ConversationID + `","via":{"run":"` + run +
		`"},"expected_version":` + strconv.FormatInt(assigned.Item.Version, 10) + `,"reason":"The person withdrew it."}`
	call := func(key, requestBody string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path, requestBody, key))
		return rec
	}
	first := call("cancel-person-run", body)
	if first.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", first.Code, first.Body)
	}
	if again := call("cancel-person-run", body); again.Code != first.Code || again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s; first %d %s", again.Code, again.Body, first.Code, first.Body)
	}
	full, err := s.workV2().Item(context.Background(), created.ID)
	if err != nil || full.Item.Phase != work.PhaseCancelled || full.Item.OwnerSession != "" {
		t.Fatalf("cancelled item: %+v, %v", full.Item, err)
	}
	for _, a := range full.Assignments {
		if a.State == "active" {
			t.Fatalf("active assignment remains: %+v", a)
		}
	}
	found := false
	for _, event := range full.Events {
		if event.Kind == "item.cancelled" && event.Actor == work.ActorViaSession+run &&
			strings.Contains(event.Payload, "The person withdrew it.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("cancellation audit missing: %+v", full.Events)
	}
	if rec := call("cancel-after-terminal", body); rec.Code != http.StatusConflict {
		t.Fatalf("stale version: %d %s", rec.Code, rec.Body)
	}
}

func TestAgentCancellationRequiresARunVersionAndReason(t *testing.T) {
	s, p, project := sessionItemServer(t)
	created := personItem(t, s, project, "feature", "cancel-refusals")
	run := issueTestRun(t, s, p, "Please remove the Queued assignment Board item")
	path := "/v1/work/v2/agent/items/" + created.ID + "/cancel"
	for _, tc := range []struct {
		name, body, code string
	}{
		{"no run", `{"session_id":"` + p.s.ConversationID + `","expected_version":1,"reason":"Withdrawn"}`, "run_unknown"},
		{"no version", `{"session_id":"` + p.s.ConversationID + `","via":{"run":"` + run + `"},"reason":"Withdrawn"}`, "version_required"},
		{"no reason", `{"session_id":"` + p.s.ConversationID + `","via":{"run":"` + run + `"},"expected_version":1}`, "reason_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path, tc.body, "cancel-"+tc.name))
			var answer struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(rec.Body.Bytes(), &answer) != nil || answer.Error != tc.code {
				t.Fatalf("refusal: %d %s", rec.Code, rec.Body)
			}
		})
	}
	item, err := s.workV2().Item(context.Background(), created.ID)
	if err != nil || string(item.Item.Phase) != created.Phase {
		t.Fatalf("refusals changed item: %+v, %v", item.Item, err)
	}
}

//go:build linux || darwin

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestTheCallbackShellRouteKeepsOwnershipAndCompletionReceipts(t *testing.T) {
	s, p := ownTodoServer(t)
	s.broker.Git = git.New()
	ctx := context.Background()
	id := "c5100000-0000-4000-8000-000000000001"
	_, err := s.broker.StartCallback(ctx, orchestrator.CallbackRequest{TaskID: id, Title: "Verify background output", Argv: []string{"sh", "-c", "printf 'background ready\\n'"}, Dir: t.TempDir(), TimeoutMinutes: 5, Root: orchestrator.RootRef{SessionID: p.s.ConversationID, Assistant: "claude"}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var before orchestrator.Record
	for {
		before, _, err = s.broker.Record(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if before.State.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callback did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	read := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/sessions/root/shells/callback-"+id, nil)
		out := httptest.NewRecorder()
		s.sessionShellRoute(out, req, p.s.ID, callbackShellPrefix+id)
		return out
	}
	out := read()
	var reply contract.ShellOutputReply
	if err := json.Unmarshal(out.Body.Bytes(), &reply); err != nil || out.Code != 200 || !reply.Ended || reply.Shell.State != "success" || !strings.Contains(reply.Text, "background ready") {
		t.Fatalf("%d %s", out.Code, out.Body)
	}
	after, _, err := s.broker.Record(ctx, id)
	if err != nil || !reflect.DeepEqual(before.Notice, after.Notice) {
		t.Fatal("view acknowledged the completion")
	}
	p.s.ConversationID = "c5100000-0000-4000-8000-000000000002"
	if out := read(); out.Code != http.StatusNotFound {
		t.Fatalf("rebound Session read another root: %d %s", out.Code, out.Body)
	}
}

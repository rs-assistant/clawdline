package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func peerMessageServer(t *testing.T) (*Server, *pane, agenthandoff.Request) {
	t.Helper()
	if swiftstore.ProcessStart(os.Getpid()).IsZero() {
		t.Skip("no kernel process-start reader")
	}
	p := &pane{s: session.Session{ID: "%19", Assistant: session.AssistantCodex, Backend: session.BackendTmux,
		PID: os.Getpid(), State: session.StateIdle, ConversationID: "10000000-0000-4000-8000-000000000001"}}
	s := paneServer(t, p)
	s.cfg.Dir = s.broker.Dir
	s.executionMachineID = "target-machine"
	endpoint, err := s.currentRemoteSession(context.Background(), p.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{p.s} }
	s.broker.Type = func(_ context.Context, terminal, text string) error { p.act(text); return nil }
	return s, p, agenthandoff.Request{RequestID: "message-1", Kind: agenthandoff.Message, GrantID: "pair-1",
		Source: agenthandoff.Endpoint{MachineID: "source-machine", SessionID: "%20", ExecutionGeneration: strings.Repeat("b", 32)}, Target: endpoint}
}

func TestPairedMessageWakesTheExactSessionOnceAndReadsItsExactInbox(t *testing.T) {
	s, p, request := peerMessageServer(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := s.PeerExecute(ctx, request, []byte("continue the task")); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.acts) != 1 || !strings.Contains(p.acts[0], "clawdline remote inbox message-1") {
		t.Fatalf("wake: %v", p.acts)
	}
	read := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/remote/inbox/message-1?session_id=%2519", nil)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Local: true}}))
		rec := httptest.NewRecorder()
		s.remoteInboxRoute(rec, req)
		return rec
	}
	if rec := read(); rec.Code != 200 || !strings.Contains(rec.Body.String(), "continue the task") {
		t.Fatalf("read: %d %s", rec.Code, rec.Body.String())
	}
	p.s.PID = 0
	if rec := read(); rec.Code == 200 || strings.Contains(rec.Body.String(), "continue the task") {
		t.Fatalf("stale read: %d %s", rec.Code, rec.Body.String())
	}
	if err := s.PeerExecute(ctx, request, []byte("continue the task")); err == nil {
		t.Fatal("stale target received a wake")
	}
	if len(p.acts) != 1 {
		t.Fatalf("stale target woke: %v", p.acts)
	}
}

func TestPairedMessageDoesNotRepeatAnUncertainComposerEffect(t *testing.T) {
	s, _, request := peerMessageServer(t)
	attempts := 0
	s.broker.Type = func(context.Context, string, string) error {
		attempts++
		return errors.New("connection lost after typing")
	}
	for i := 0; i < 2; i++ {
		if err := s.PeerExecute(context.Background(), request, []byte("continue")); err == nil {
			t.Fatal("uncertain delivery claimed success")
		}
	}
	if attempts != 1 {
		t.Fatalf("uncertain effect retried %d times", attempts)
	}
}

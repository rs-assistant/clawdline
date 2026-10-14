package http

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app"
)

func TestSecretHandoffOnlyTargetMayClaimOnce(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	s.secretHandoffs = app.NewSecretHandoffs()
	createBody, _ := json.Marshal(map[string]any{
		"source_conversation": source, "target_session": "pane-target", "kind": "secret",
		"title": "Database credential", "summary": "A local tool needs a credential.",
		"action": "Paste it into Secret Handoff.", "reason": "The tool cannot continue without it.",
	})
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", string(createBody), "secret-note"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var result struct {
		Note humanInterventionWire `json:"note"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	id := result.Note.ID
	base := "/v1/work/v2/human-interventions/conversation:" + target + "/" + id + "/handoff"
	secret := "SAFE_FIXTURE_DONT_STORE_123"
	wrong := httptest.NewRecorder()
	s.workV2Route(wrong, agentWorkV2Request(http.MethodPost, base, `{"value":"`+secret+`"}`, "wrong-actor"))
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("agent submitted: %d %s", wrong.Code, wrong.Body)
	}
	submitted := httptest.NewRecorder()
	s.workV2Route(submitted, personWorkV2Request(http.MethodPost, base, `{"value":"`+secret+`"}`, "secret-submit"))
	if submitted.Code != http.StatusOK || strings.Contains(submitted.Body.String(), secret) {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body)
	}
	note, err := s.store.HumanIntervention(t.Context(), id)
	if err != nil || strings.Contains(note.Title+note.Summary+note.Action+note.Reason+note.Detail+note.Resolution, secret) {
		t.Fatalf("Note stored value: %v", err)
	}
	claimPath := "/v1/work/v2/agent/human-interventions/" + id + "/claim"
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	nonce := []byte("0123456789abcdef0123456789abcdef")
	callback := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			callback <- "accept failed"
			return
		}
		defer conn.Close()
		presented := make([]byte, 32)
		if _, err := io.ReadFull(conn, presented); err != nil || string(presented) != string(nonce) {
			callback <- "bad nonce"
			return
		}
		var size [4]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			callback <- "bad size"
			return
		}
		value := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(conn, value); err != nil {
			callback <- "bad value"
			return
		}
		_, _ = conn.Write([]byte{1})
		callback <- string(value)
	}()
	claimBody := func(conversation string) string {
		return fmt.Sprintf(`{"conversation_id":%q,"callback_port":%d,"nonce":%q}`, conversation,
			listener.Addr().(*net.TCPAddr).Port, base64.RawURLEncoding.EncodeToString(nonce))
	}
	other := httptest.NewRecorder()
	s.workV2Route(other, agentWorkV2Request(http.MethodPost, claimPath, claimBody(source), "wrong-target"))
	if other.Code != http.StatusNotFound {
		t.Fatalf("wrong target: %d %s", other.Code, other.Body)
	}
	claim := httptest.NewRecorder()
	s.workV2Route(claim, agentWorkV2Request(http.MethodPost, claimPath, claimBody(target), "claim"))
	gotCallback := ""
	select {
	case gotCallback = <-callback:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not arrive")
	}
	if claim.Code != http.StatusOK || strings.Contains(claim.Body.String(), secret) || claim.Header().Get("Cache-Control") != "no-store" || gotCallback != secret {
		t.Fatalf("claim: %d %q", claim.Code, claim.Body.String())
	}
	again := httptest.NewRecorder()
	s.workV2Route(again, agentWorkV2Request(http.MethodPost, claimPath, claimBody(target), "again"))
	if again.Code != http.StatusConflict || strings.Contains(again.Body.String(), secret) {
		t.Fatalf("reclaimed: %d %s", again.Code, again.Body)
	}
	note, err = s.store.HumanIntervention(t.Context(), id)
	if err != nil || note.ResolvedAt == nil || note.Resolution != "Secret Handoff used" {
		t.Fatalf("not handled: %+v %v", note, err)
	}
	if err := filepath.WalkDir(s.broker.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), secret) {
			t.Errorf("secret reached a durable file: %s", entry.Name())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

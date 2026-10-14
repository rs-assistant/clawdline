package main

import (
	"bytes"
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
)

func TestSecretHandoffConsumerHelper(t *testing.T) {
	if os.Getenv("CLAWDLINE_SECRET_TEST_HELPER") != "1" {
		return
	}
	value, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(4)
	}
	if err := os.WriteFile(os.Getenv("CLAWDLINE_SECRET_TEST_FILE"), value, 0600); err != nil {
		os.Exit(5)
	}
	_, _ = os.Stdout.Write(value) // runSecret must suppress even a consumer that echoes it.
	_, _ = os.Stderr.Write(value)
	os.Exit(0)
}

func TestSecretRunFeedsStdinAndSuppressesConsumerOutput(t *testing.T) {
	const secret = "SAFE_FIXTURE_FOR_STDIN"
	const id = "10000000-0000-4000-8000-000000000003"
	const conversation = "10000000-0000-4000-8000-000000000002"
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/work/v2/agent/human-interventions/"+id+"/claim" || r.Header.Get("X-Clawdline-Orchestrator") != "machine-fixture" {
			t.Errorf("unexpected claim: %s", r.URL.Path)
		}
		var claim struct {
			CallbackPort   int    `json:"callback_port"`
			Nonce          string `json:"nonce"`
			ConversationID string `json:"conversation_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&claim); err != nil || claim.ConversationID != conversation {
			t.Errorf("invalid claim: %v", err)
		}
		nonce, err := base64.RawURLEncoding.DecodeString(claim.Nonce)
		if err != nil {
			t.Errorf("nonce: %v", err)
		}
		conn, err := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", claim.CallbackPort))
		if err != nil {
			t.Errorf("callback: %v", err)
			return
		}
		defer conn.Close()
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(secret)))
		_, _ = conn.Write(nonce)
		_, _ = conn.Write(size[:])
		_, _ = io.WriteString(conn, secret)
		var ack [1]byte
		if _, err := io.ReadFull(conn, ack[:]); err != nil || ack[0] != 1 {
			t.Errorf("ack: %v %d", err, ack[0])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"delivered":true}`)
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "received")
	t.Setenv("CLAWDLINE_SECRET_TEST_HELPER", "1")
	t.Setenv("CLAWDLINE_SECRET_TEST_FILE", file)
	b := &broker{base: server.URL, token: "machine-fixture", client: server.Client()}
	var out, errs bytes.Buffer
	code := runSecret(&out, &errs, b, id, []string{os.Args[0], "-test.run=TestSecretHandoffConsumerHelper"}, func(k string) string {
		if k == "CODEX_THREAD_ID" {
			return conversation
		}
		return ""
	})
	got, err := os.ReadFile(file)
	if code != 0 || err != nil || string(got) != secret || requests != 1 {
		t.Fatalf("run: code=%d file=%q requests=%d err=%v", code, got, requests, err)
	}
	if strings.Contains(out.String()+errs.String(), secret) {
		t.Fatal("consumer echoed the secret into the agent transcript")
	}
}

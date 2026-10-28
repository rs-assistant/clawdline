package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNoteCreateInjectsIdentityAndPreservesRetryKey(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.URL.Path != "/v1/work/v2/agent/human-interventions" || r.Method != http.MethodPost ||
			r.Header.Get("X-Clawdline-Orchestrator") != "machine-secret" || r.Header.Get("Idempotency-Key") != "same-press" {
			t.Errorf("the note did not use the Agent route and credential: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["source_conversation"] != "root-conversation" || body["target_session"] != "target-terminal" ||
			body["title"] != "Choose a date" {
			t.Errorf("the note lost its source, target, or content: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"note":{"id":"note-id"}}`))
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	code := createNote(&out, &errs, b, "target-terminal", "", []byte(`{"kind":"answer","title":"Choose a date"}`), "same-press", false,
		func(name string) string {
			if name == conversationEnv[0] {
				return "root-conversation"
			}
			return ""
		})
	if code != 0 || called != 1 || !strings.Contains(out.String(), "note-id") {
		t.Fatalf("note create: code %d, calls %d, output %q, errors %q", code, called, out.String(), errs.String())
	}
}

func TestNoteCreateRejectsCallerSuppliedIdentity(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	code := createNote(&out, &errs, b, "target-terminal", "root-conversation",
		[]byte(`{"source_conversation":"somebody-else","kind":"answer"}`), "", false,
		func(string) string { return "" })
	if code != 2 || called != 0 || !strings.Contains(errs.String(), "source_conversation") {
		t.Fatalf("identity spoof was sent: code %d, calls %d, errors %q", code, called, errs.String())
	}
}

func TestNoteCreateTargetsTheCallingSessionByDefault(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/orchestrator/whoami" {
			if r.Method != http.MethodGet || r.URL.Query().Get("conversation_id") != "root-conversation" {
				t.Errorf("wrong whoami request: %s %s", r.Method, r.URL.String())
			}
			_, _ = w.Write([]byte(`{"terminal_id":"own-terminal"}`))
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["target_session"] != "own-terminal" || body["source_conversation"] != "root-conversation" {
			t.Errorf("note used the wrong identities: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"note":{"id":"note-id"}}`))
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	code := createNote(&out, &errs, b, "", "root-conversation", []byte(`{"kind":"answer","title":"Choose a date"}`), "same-press", false, func(string) string { return "" })
	if code != 0 || len(paths) != 2 || paths[0] != "/v1/orchestrator/whoami" || paths[1] != "/v1/work/v2/agent/human-interventions" {
		t.Fatalf("note create: code %d, paths %v, output %q, errors %q", code, paths, out.String(), errs.String())
	}
}

func TestNoteCreateDoesNotPostWhenOwnSessionCannotBeResolved(t *testing.T) {
	for _, reply := range []string{`{"ok":true}`, `{"terminal_id":""}`} {
		called := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			if r.URL.Path != "/v1/orchestrator/whoami" {
				t.Errorf("unexpected note post: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(reply))
		}))
		b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
		var out, errs bytes.Buffer
		code := createNote(&out, &errs, b, "", "root-conversation", []byte(`{"kind":"answer"}`), "", false, func(string) string { return "" })
		server.Close()
		if code != 1 || called != 1 || !strings.Contains(errs.String(), "without a terminal id") {
			t.Fatalf("unresolved target: code %d, calls %d, errors %q", code, called, errs.String())
		}
	}
}

func TestNoteCreateMarksOnlyExplicitlyRequestedNotes(t *testing.T) {
	var requested any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requested = body["requested_by_person"]
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"note":{"id":"note-id"}}`))
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	input := []byte(`{"kind":"read","title":"Read this"}`)
	if code := createNote(&out, &errs, b, "target", "source", input, "note-one", true, func(string) string { return "" }); code != 0 || requested != true {
		t.Fatalf("explicit request: code=%d marker=%v errors=%s", code, requested, errs.String())
	}
	requested = nil
	if code := createNote(&out, &errs, b, "target", "source", input, "note-two", false, func(string) string { return "" }); code != 0 || requested != nil {
		t.Fatalf("agent initiated: code=%d marker=%v errors=%s", code, requested, errs.String())
	}
	if code := createNote(&out, &errs, b, "target", "source", []byte(`{"requested_by_person":true}`), "note-three", false, func(string) string { return "" }); code != 2 {
		t.Fatalf("body spoofed request marker: code=%d", code)
	}
}

// A Secret Handoff Note names its receiving command by the path PATH finds in
// the Agent's environment, so the person reads which file will run.
func TestNoteCreateNamesTheSecretCommandByItsPath(t *testing.T) {
	dir := t.TempDir()
	tool := executableIn(t, dir, "credential-tool", filepath.Join(dir, "ran"))
	t.Setenv("PATH", dir)
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Command []string `json:"command"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = body.Command
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"note":{"id":"note-id"}}`))
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	code := createNote(&out, &errs, b, "target-terminal", "root-conversation",
		[]byte(`{"kind":"secret","title":"Key","command":["credential-tool","--with-token"]}`), "k", false, func(string) string { return "" })
	if code != 0 || !slices.Equal(sent, []string{tool, "--with-token"}) {
		t.Fatalf("note create: code %d, sent %q, errors %q", code, sent, errs.String())
	}
	sent = nil
	code = createNote(&out, &errs, b, "target-terminal", "root-conversation",
		[]byte(`{"kind":"secret","title":"Key","command":["no-such-tool-anywhere"]}`), "k2", false, func(string) string { return "" })
	if code != 2 || sent != nil || !strings.Contains(errs.String(), "no-such-tool-anywhere") {
		t.Fatalf("an unknown command was sent: code %d, sent %q, errors %q", code, sent, errs.String())
	}
}

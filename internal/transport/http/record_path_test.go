package http

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// movedClaudeSession is a Claude session whose process now sits in a
// different directory from the one it was launched in. Claude Code filed its
// record under the launch directory, which only the registry still names; the
// process table reports where the process is now.
func movedClaudeSession(t *testing.T) (*transcript.Host, session.Session, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	launched := filepath.Join(home, "launched")
	moved := filepath.Join(home, "launched", "moved")
	const pid = 4242
	const conv = "0c0ffee0-0000-4000-8000-00000000c0de"
	registry := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(registry, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := `{"pid":4242,"sessionId":"` + conv + `","cwd":"` + launched + `"}`
	if err := os.WriteFile(filepath.Join(registry, strconv.Itoa(pid)+".json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	record := transcript.ClaudePath(home, launched, conv)
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","timestamp":"2026-10-18T00:00:00.000Z","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(record, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	host := transcript.NewHost()
	host.Home = home
	host.Refresh()
	item := session.Session{ID: "%42", PID: pid, Assistant: session.AssistantClaude, CWD: moved, ConversationID: conv}
	return host, item, record, transcript.ClaudePath(home, moved, conv)
}

// Pins the behaviour before one resolver existed: the conversation reader
// composed its path from the scanned directory while the host used the
// registry's, so one Session had two records.
func TestOneSessionHasTwoRecordsBeforeTheResolver(t *testing.T) {
	host, item, launchedRecord, movedRecord := movedClaudeSession(t)
	if got := recordPath(item); got != movedRecord {
		t.Fatalf("reader path = %q, want the scanned directory's %q", got, movedRecord)
	}
	if a := host.LastActivity(t.Context(), item); a.Reason != "" {
		t.Fatalf("host did not find the record at %q: %+v", launchedRecord, a)
	}
	if _, err := os.Stat(movedRecord); !os.IsNotExist(err) {
		t.Fatalf("the reader's path should not exist: %v", err)
	}
}

package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	childTask  = "a1111111-1111-4111-8111-111111111111"
	otherTask  = "a2222222-2222-4222-8222-222222222222"
	childConv  = "b1111111-1111-4111-8111-111111111111"
	personConv = "b2222222-2222-4222-8222-222222222222"
	blockConv  = "b3333333-3333-4333-8333-333333333333"
)

func writeTranscript(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A child's conversation is the one whose first message is the broker's line
// naming its task; a person's conversation that only mentions the task later
// is not.
func TestAChildConversationIsFoundByItsFirstLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := "/work/project"
	dir := filepath.Join(home, ".claude", "projects", Slug(project))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, dir, childConv,
		`{"type":"summary","summary":"x"}`,
		`{"type":"user","message":{"content":"`+ChildFirstLinePrefix+childTask+`. Say this line first"}}`)
	writeTranscript(t, dir, personConv,
		`{"type":"user","message":{"content":"Why was task `+childTask+` closed?"}}`,
		`{"type":"user","message":{"content":"`+ChildFirstLinePrefix+childTask+`."}}`)
	writeTranscript(t, dir, blockConv,
		`{"type":"user","message":{"content":[{"type":"text","text":"`+ChildFirstLinePrefix+otherTask+`. Say"}]}}`)

	got := ClaudeChildConversations(project, time.Now().Add(-time.Hour), map[string]bool{childTask: true, otherTask: true})
	if got[childTask] != childConv || got[otherTask] != blockConv || len(got) != 2 {
		t.Fatalf("found %v, want %s → %s and %s → %s", got, childTask, childConv, otherTask, blockConv)
	}
	if task, ok := ClaudeChildTask(project, personConv); ok {
		t.Errorf("a person's conversation was read as the child of %s", task)
	}
	if task, ok := ClaudeChildTask(project, childConv); !ok || task != childTask {
		t.Errorf("ClaudeChildTask = %q %v, want %s", task, ok, childTask)
	}
	if got := ClaudeChildConversations(project, time.Now().Add(time.Hour), map[string]bool{childTask: true}); len(got) != 0 {
		t.Errorf("a transcript last written before the run began was matched: %v", got)
	}
}

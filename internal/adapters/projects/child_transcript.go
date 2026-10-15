package projects

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ChildFirstLinePrefix is how the first message a broker types into a child
// begins, followed by the task id (orchestrator.FirstLine says it with this
// constant). A conversation whose first message from the person's side is
// that line is the child of that task: nothing else on the machine types it.
const ChildFirstLinePrefix = "You are a Clawdline CHILD agent for task "

// MaxChildTranscriptsProbed is how many of a project's Claude transcripts one
// lookup opens, newest first. A scheduled run's transcript is written while
// it runs, so it is among the newest written since that run began; a project
// with more sessions than this since then is answered without the older ones,
// and the run simply offers no resume.
const MaxChildTranscriptsProbed = 256

// MaxChildTranscriptHead is how much of one transcript is read to find its
// first message. The lines before it are a few short records of how the
// session started.
const MaxChildTranscriptHead = 64 << 10

// ClaudeChildConversations finds, among the Claude transcripts of projectDir
// written to since since, the conversations whose first message briefed one
// of the tasks in want, and answers task id → conversation id. It is how a
// scheduled run from before the broker recorded its child's conversation is
// matched to the conversation it had.
func ClaudeChildConversations(projectDir string, since time.Time, want map[string]bool) map[string]string {
	out := map[string]string{}
	h := home()
	if h == "" || projectDir == "" || len(want) == 0 {
		return out
	}
	dir := filepath.Join(h, ".claude", "projects", Slug(projectDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	type candidate struct {
		id string
		at time.Time
	}
	found := []candidate{}
	for _, e := range entries {
		name := e.Name()
		id, ok := SessionName(strings.TrimSuffix(name, ".jsonl"))
		if !ok || !strings.HasSuffix(name, ".jsonl") || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		found = append(found, candidate{id, info.ModTime()})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	if len(found) > MaxChildTranscriptsProbed {
		found = found[:MaxChildTranscriptsProbed]
	}
	for _, c := range found {
		task, ok := claudeChildTaskAt(filepath.Join(dir, c.id+".jsonl"))
		if !ok || !want[task] {
			continue
		}
		if _, taken := out[task]; !taken {
			out[task] = c.id
		}
		if len(out) == len(want) {
			break
		}
	}
	return out
}

// ClaudeChildTask is the task whose child conversation id is, in
// projectDir: the task its first message briefed, or false when it is not a
// child's conversation or its transcript is not there.
func ClaudeChildTask(projectDir, id string) (string, bool) {
	id, ok := SessionName(id)
	h := home()
	if !ok || h == "" || projectDir == "" {
		return "", false
	}
	return claudeChildTaskAt(filepath.Join(h, ".claude", "projects", Slug(projectDir), id+".jsonl"))
}

func claudeChildTaskAt(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	return claudeChildTask(io.LimitReader(f, MaxChildTranscriptHead))
}

// claudeChildTask reads a transcript up to its first message from the user's
// side and answers the task that message briefed.
func claudeChildTask(r io.Reader) (string, bool) {
	lines := bufio.NewReader(r)
	for {
		line, err := lines.ReadBytes('\n')
		if err != nil && (err != io.EOF || len(line) == 0) {
			return "", false
		}
		var row struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &row) == nil && row.Type == "user" {
			return childTaskOf(firstText(row.Message.Content))
		}
		if err == io.EOF {
			return "", false
		}
	}
}

// firstText is a message's text: the string itself, or its first text block.
func firstText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" {
				return b.Text
			}
		}
	}
	return ""
}

func childTaskOf(text string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), ChildFirstLinePrefix)
	if !ok || len(rest) < 36 {
		return "", false
	}
	return SessionName(rest[:36])
}

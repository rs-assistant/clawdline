//go:build !windows

package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func historyProvider(t *testing.T, response string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	exe, requests := filepath.Join(dir, "provider"), filepath.Join(dir, "requests")
	script := "#!/bin/sh\nwhile IFS= read -r line; do\nprintf '%s\\n' \"$line\" >> '" + requests + "'\ncase \"$line\" in\n*initialize*0*|*0*initialize*) echo '{\"id\":0,\"result\":{}}';;\n*thread/list*|*thread/read*) echo '" + response + "';;\nesac\ndone\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return exe, requests
}

func TestCodexPageAdvancesPastFilteredChildren(t *testing.T) {
	exe, requests := historyProvider(t, `{"id":1,"result":{"data":[{"id":"00000000-0000-4000-8000-000000000001","cwd":"/project","updatedAt":10,"preview":"`+BriefingOpening+` child"}],"nextCursor":"older"}}`)
	rows, next, err := codexPastPage(context.Background(), exe, Place{Path: "/project"}, nil, PastTitles{}, "newer")
	if err != nil || len(rows) != 0 || next != "older" {
		t.Fatalf("rows=%v next=%q err=%v", rows, next, err)
	}
	raw, _ := os.ReadFile(requests)
	for _, want := range []string{`"limit":20`, `"cursor":"newer"`, `"cwd":"/project"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
}

func TestCodexPageFailureIsNeverAnEmptySuccess(t *testing.T) {
	for _, reply := range []string{
		`{"id":1,"error":{"code":-1,"message":"unavailable"}}`,
		`{"id":1,"result":{}}`,
		`{"id":1,"result":{"data":{}}}`,
		`{"id":1,"result":{"data":[],"nextCursor":"same"}}`,
	} {
		t.Run(reply, func(t *testing.T) {
			exe, _ := historyProvider(t, reply)
			_, _, err := codexPastPage(context.Background(), exe, Place{Path: "/project"}, nil, PastTitles{}, "same")
			if !errors.Is(err, ErrCodexUnavailable) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCodexPageHonorsCancellation(t *testing.T) {
	exe, _ := historyProvider(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := codexPastPage(ctx, exe, Place{Path: "/project"}, nil, PastTitles{}, "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("provider failed to stop on cancellation: %v", err)
	}
}

func TestCodexResumeReadsOnlyTheSelectedThreadAndChecksItsProject(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000002"
	for _, cwd := range []string{"/project", "/another-project"} {
		exe, requests := historyProvider(t, `{"id":1,"result":{"thread":{"id":"`+id+`","cwd":"`+cwd+`","source":"cli","updatedAt":10,"preview":"An ordinary conversation"}}}`)
		found, err := codexConversation(context.Background(), exe, Place{Path: "/project"}, id)
		if err != nil || found != (cwd == "/project") {
			t.Fatalf("cwd=%s found=%v err=%v", cwd, found, err)
		}
		raw, _ := os.ReadFile(requests)
		if strings.Contains(string(raw), "thread/list") || !strings.Contains(string(raw), `"includeTurns":false`) || !strings.Contains(string(raw), `"threadId":"`+id+`"`) {
			t.Fatalf("wrong request %s", raw)
		}
	}
	exe, _ := historyProvider(t, `{"id":1,"error":{"code":-1,"message":"unavailable"}}`)
	if found, err := codexConversation(context.Background(), exe, Place{Path: "/project"}, id); found || !errors.Is(err, ErrCodexUnavailable) {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

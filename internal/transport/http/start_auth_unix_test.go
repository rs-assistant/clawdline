//go:build !windows

package http

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestFirstGraphicalStartRecognizesOnlyProvenSignedOutAgents(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "")
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "")
	t.Setenv("OPENAI_API_KEY", "")
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("codex", "echo 'Not logged in' >&2; exit 1")
	write("claude", "echo '{\"loggedIn\":false,\"apiProvider\":\"firstParty\"}'; exit 1")
	if !assistantNeedsSignIn(context.Background(), projects.AssistantCodex) ||
		!assistantNeedsSignIn(context.Background(), projects.AssistantClaude) {
		t.Fatal("a known signed-out CLI would open an unhelpful terminal prompt")
	}
	write("codex", "echo 'Logged in using ChatGPT'; exit 0")
	write("claude", "echo '{\"loggedIn\":true,\"apiProvider\":\"firstParty\"}'; exit 0")
	if assistantNeedsSignIn(context.Background(), projects.AssistantCodex) ||
		assistantNeedsSignIn(context.Background(), projects.AssistantClaude) {
		t.Fatal("a signed-in CLI must remain launchable")
	}
	write("claude", "echo '{\"loggedIn\":false,\"apiProvider\":\"bedrock\"}'; exit 1")
	if assistantNeedsSignIn(context.Background(), projects.AssistantClaude) {
		t.Fatal("another Claude provider must not be treated as signed out")
	}
	write("codex", "echo 'error: unknown command status' >&2; exit 2")
	write("claude", "echo 'error: unknown command status' >&2; exit 2")
	if assistantNeedsSignIn(context.Background(), projects.AssistantCodex) ||
		assistantNeedsSignIn(context.Background(), projects.AssistantClaude) {
		t.Fatal("an older CLI without a status command must not be called signed out")
	}
	write("codex", "echo 'Not logged in' >&2; exit 1")
	t.Setenv("OPENAI_API_KEY", "test-only-key")
	if assistantNeedsSignIn(context.Background(), projects.AssistantCodex) {
		t.Fatal("a key-based launch must not be blocked by CLI sign-in")
	}
}

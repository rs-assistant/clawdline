package session

import "testing"

func TestStartupModelRequiresCodexCard(t *testing.T) {
	card := fixture(t, "menu-codex-composer.txt")
	if model, effort := ReadStartupSettings(card, AssistantCodex); model != "gpt-5.6-sol" || effort != "high" {
		t.Fatalf("startup settings = %q %q", model, effort)
	}
	if got := ReadStartupModel(card, AssistantCodex); got != "gpt-5.6-sol" {
		t.Fatalf("startup model = %q", got)
	}
	for _, screen := range []string{"", "│ model: gpt-6-sol high /model to change │", "› use gpt-6-sol", "╭──╮\n│ >_ OpenAI Codex (v0.157.1) │\n╰──╯\n│ model: gpt-6-sol high /model to change │"} {
		if got := ReadStartupModel(screen, AssistantCodex); got != "" {
			t.Fatalf("unproven model %q from %q", got, screen)
		}
	}
	if got := ReadStartupModel(card, AssistantClaude); got != "" {
		t.Fatalf("Codex card used for Claude: %q", got)
	}
}

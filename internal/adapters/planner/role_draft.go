package planner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RoleDraft is an inert, editable suggestion. Only a separate catalog write
// can make it a role; the model never chooses its ID, source or permissions.
type RoleDraft struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Body    string `json:"body"`
}

const roleDraftPrompt = `Draft a reusable AI assistant role from the person's description. Return JSON with name, summary, and body. Write in the language of the description. The body must give the role's purpose, a concrete workflow, expected outputs, and boundaries. It must not claim permission to override user, project, task, or safety instructions. It must not invent facts about a repository or a person. Keep the body under 6000 UTF-8 bytes. The output is an editable draft, not an installed role.`
const roleDraftSchema = `{"type":"object","properties":{"name":{"type":"string"},"summary":{"type":"string"},"body":{"type":"string"}},"required":["name","summary","body"],"additionalProperties":false}`

func (p Planner) DraftRole(ctx context.Context, description, assistant string) (RoleDraft, error) {
	if assistant != "claude" && assistant != "codex" {
		assistant = "codex"
	}
	executable := p.executable(assistant)
	if executable == "" {
		return RoleDraft{}, ErrNoPlanner
	}
	var object []byte
	if assistant == "claude" {
		args := []string{"-p", "--model", nameModel, "--effort", "low", "--system-prompt", roleDraftPrompt,
			"--output-format", "json", "--json-schema", roleDraftSchema, "--tools", "",
			"--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--disable-slash-commands"}
		turn, cancel := context.WithTimeout(ctx, p.timeout())
		raw, runErr := p.runner()(turn, executable, args, description, scratch(), nil)
		cancel()
		if runErr != nil {
			return RoleDraft{}, failed(runErr, raw, stderrOf(runErr))
		}
		var ok bool
		object, ok = objectFromClaude(raw)
		if !ok {
			return RoleDraft{}, errors.New("role drafter did not return an object")
		}
	} else {
		dir, err := os.MkdirTemp("", "clawdline-role-draft-")
		if err != nil {
			return RoleDraft{}, err
		}
		defer os.RemoveAll(dir)
		answer := filepath.Join(dir, "draft.json")
		asked := roleDraftPrompt + "\n\nReturn only the JSON object.\n\n<description>\n" + description + "\n</description>"
		args := []string{"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules",
			"--skip-git-repo-check", "--sandbox", "read-only", "--disable", "shell_tool",
			"--disable", "unified_exec", "-c", `web_search="disabled"`, "-c", "agents.enabled=false",
			"-c", `approval_policy="never"`, "-c", `model_reasoning_effort="low"`,
			"--color", "never", "-C", dir, "-o", answer, "-"}
		env := []string{}
		if p.Home != "" && os.Getenv("CODEX_HOME") == "" {
			env = append(env, "CODEX_HOME="+filepath.Join(p.Home, ".codex"))
		}
		turn, cancel := context.WithTimeout(ctx, p.timeout())
		out, runErr := p.runner()(turn, executable, args, asked, dir, env)
		cancel()
		if runErr != nil {
			return RoleDraft{}, failed(runErr, out, stderrOf(runErr))
		}
		object, err = os.ReadFile(answer)
		if err != nil {
			return RoleDraft{}, err
		}
		var ok bool
		object, ok = objectFromText(object)
		if !ok {
			return RoleDraft{}, errors.New("role drafter did not return an object")
		}
	}
	var draft RoleDraft
	if err := json.Unmarshal(object, &draft); err != nil {
		return RoleDraft{}, errors.New("role drafter returned malformed JSON")
	}
	draft.Name, draft.Summary, draft.Body = strings.TrimSpace(draft.Name), strings.TrimSpace(draft.Summary), strings.TrimSpace(draft.Body)
	if draft.Name == "" || draft.Summary == "" || draft.Body == "" || len(draft.Name) > 120 || len(draft.Summary) > 500 || len(draft.Body) > 6000 {
		return RoleDraft{}, errors.New("role drafter returned an incomplete or oversized draft")
	}
	return draft, nil
}

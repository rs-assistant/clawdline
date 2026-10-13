package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

func TestRoleDraftNeedsAnExplicitWriteCapablePressAndReplaysOneTurn(t *testing.T) {
	f := newSquadFixture(t)
	runs := 0
	f.s.draftRole = func(_ context.Context, description, provider string) (planner.RoleDraft, error) {
		runs++
		if description != "Review release notes" || provider != "codex" {
			t.Fatalf("description=%q provider=%q", description, provider)
		}
		return planner.RoleDraft{Name: "Release editor", Summary: "Reviews release notes", Body: "Check claims against the source."}, nil
	}
	if status, _ := f.ask(http.MethodGet, "/v1/squad/catalog", f.reader, "", ""); status != http.StatusOK || runs != 0 {
		t.Fatalf("read status=%d runs=%d", status, runs)
	}
	if status, _ := f.ask(http.MethodPost, "/v1/squad/role-draft", "", `{"description":"Review release notes"}`, "anonymous-draft"); status != http.StatusUnauthorized || runs != 0 {
		t.Fatalf("anonymous status=%d runs=%d", status, runs)
	}
	firstStatus, first := f.ask(http.MethodPost, "/v1/squad/role-draft", f.sender, `{"description":"Review release notes"}`, "draft-once")
	againStatus, again := f.ask(http.MethodPost, "/v1/squad/role-draft", f.sender, `{"description":"Review release notes"}`, "draft-once")
	if firstStatus != http.StatusOK || againStatus != http.StatusOK || first != again || runs != 1 {
		t.Fatalf("first=%d %s again=%d %s runs=%d", firstStatus, first, againStatus, again, runs)
	}
	if strings.Contains(first, "definition_id") {
		t.Fatal("an AI draft installed a role")
	}
}

func TestCreatedRoleAppearsInCustomPickerAndLaunches(t *testing.T) {
	f := newSquadFixture(t)
	if status, raw := f.ask(http.MethodGet, "/v1/personas", f.reader, "", ""); status != http.StatusOK || strings.Contains(raw, "personal.persona.release-editor") {
		t.Fatalf("before=%d %s", status, raw)
	}
	icon := squad.Builtins().Definitions[0].Icon
	definition := squad.Definition{
		DefinitionID: "personal.persona.release-editor", Version: "1.0.0", Source: "person", License: "UNLICENSED",
		Name:    squad.Names{En: "Release editor", ZhHant: "版本主編"},
		Summary: squad.Names{En: "Reviews release notes", ZhHant: "檢查版本說明"},
		Body:    "Check claims against the source.", Icon: icon,
		Teams: []squad.Reference{{ID: "clawdline.team.custom", Version: "1"}}, Skills: []squad.SkillReference{},
	}
	request, _ := json.Marshal(map[string]any{"kind": "definition", "expected_version": 0,
		"entity": map[string]any{"definition": definition}})
	if status, raw := f.ask(http.MethodPost, "/v1/squad/catalog", f.sender, string(request), "create-release-editor"); status != http.StatusOK {
		t.Fatalf("write=%d %s", status, raw)
	}
	status, raw := f.ask(http.MethodGet, "/v1/personas", f.reader, "", "")
	var listed contract.PersonaCatalog
	if status != http.StatusOK || json.Unmarshal([]byte(raw), &listed) != nil {
		t.Fatalf("list=%d %s", status, raw)
	}
	found := false
	for _, p := range listed.Personas {
		if p.ID == definition.DefinitionID {
			found = len(p.Teams) == 1 && p.Teams[0] == "custom" && p.Name.ZhHant == "版本主編"
		}
	}
	if !found {
		t.Fatalf("custom role missing from picker: %s", raw)
	}
	snapshot, err := f.s.ResolveSquadLaunch(context.Background(), definition.DefinitionID, "")
	if err != nil || snapshot.DefinitionID != definition.DefinitionID || snapshot.Definition.Body != definition.Body {
		t.Fatalf("launch=%+v err=%v", snapshot, err)
	}
}

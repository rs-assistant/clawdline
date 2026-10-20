package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/squad"
)

func TestSquadCatalogCLIKeepsKeysCredentialsAndTypedErrors(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 409, `{"error":"version_conflict","current_version":7,"detail":"reload"}`
	})
	file := filepath.Join(t.TempDir(), "bind.json")
	os.WriteFile(file, []byte(`{"definition_id":"personal.persona.editor","expected_version":0,"overrides":{"auto_assign":{"present":true,"value":true}}}`), 0600)
	var out, errs bytes.Buffer
	code := runSquadCatalog(&out, &errs, []string{"bind", "--body-file", file, "--key", "stable-key", "--json"}, b)
	seen := s.requests()
	if code != 1 || !strings.Contains(out.String(), `"current_version":7`) || len(seen) != 1 || seen[0].Method != "PUT" || seen[0].Key != "stable-key" || seen[0].Token != thinToken || strings.Contains(out.String()+errs.String(), thinToken) {
		t.Fatalf("code=%d out=%s err=%s seen=%+v", code, out.String(), errs.String(), seen)
	}
}

func TestSquadIconCLIPreservesDefinitionAndPinnedSkills(t *testing.T) {
	original := squad.Definition{DefinitionID: "personal.persona.editor", Version: "1", Source: "Original", License: "UNLICENSED", Digest: "old", Name: squad.Names{En: "Editor"}, Body: "Keep this body", Skills: []squad.SkillReference{{ID: "personal.skill.proof", Version: "3", Enabled: true}}}
	catalog, _ := json.Marshal(squad.Catalog{CatalogVersion: 5, Definitions: []squad.Definition{original}})
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == "GET" {
			return 200, string(catalog)
		}
		return 200, `{"id":"personal.persona.editor","version":"2"}`
	})
	icon := squad.Builtins().Definitions[0].Icon
	data, _ := json.Marshal(icon)
	file := filepath.Join(t.TempDir(), "icon.json")
	os.WriteFile(file, data, 0600)
	var out, errs bytes.Buffer
	code := runSquadCatalog(&out, &errs, []string{"icon", "--id", original.DefinitionID, "--version", "2", "--expected-version", "5", "--body-file", file, "--key", "icon-stable"}, b)
	seen := s.requests()
	if code != 0 || len(seen) != 2 {
		t.Fatalf("code=%d err=%s seen=%+v", code, errs.String(), seen)
	}
	var req struct {
		Expected int `json:"expected_version"`
		Entity   struct {
			Definition squad.Definition `json:"definition"`
		} `json:"entity"`
	}
	if json.Unmarshal(seen[1].Body, &req) != nil || req.Expected != 5 || req.Entity.Definition.Body != original.Body || req.Entity.Definition.Skills[0] != original.Skills[0] || req.Entity.Definition.Digest != "" || req.Entity.Definition.Version != "2" {
		t.Fatalf("request=%s", seen[1].Body)
	}
}

func TestSquadGuideIsDiscoverableAndCompleteInBothLanguages(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hant"} {
		for _, part := range []string{"squad", "persona"} {
			var out, errs bytes.Buffer
			if code := printGuide(&out, &errs, []string{lang, part}); code != 0 {
				t.Fatal(errs.String())
			}
			for _, want := range []string{"X-Clawdline-Orchestrator", "expected_version", "Idempotency-Key", "personal.persona.source-editor", "icon.json", "unknown_reference"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("%s %s missing %s", lang, part, want)
				}
			}
		}
	}
}

func TestSquadIconCLIRetainsTypedReadRefusalInJSONMode(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) { return 401, `{"error":"unauthorized"}` })
	file := filepath.Join(t.TempDir(), "icon.json")
	os.WriteFile(file, []byte(`{"accent":"#d97935","cells":[]}`), 0600)
	var out, errs bytes.Buffer
	code := runSquadCatalog(&out, &errs, []string{"icon", "--id", "personal.persona.editor", "--version", "2", "--expected-version", "5", "--body-file", file, "--key", "icon-once", "--json"}, b)
	if code != 1 || !strings.Contains(out.String(), `"error":"unauthorized"`) {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errs.String())
	}
}

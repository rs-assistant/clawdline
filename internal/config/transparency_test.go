package config_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type transparencyCatalog struct {
	Schema int `json:"schema"`
	Cards  []struct {
		ID      string `json:"id"`
		Sources []struct {
			Path string `json:"path"`
		} `json:"sources"`
	} `json:"cards"`
	SourceChecks map[string]string `json:"sourceChecks"`
	CopyChecks   map[string]string `json:"copyChecks"`
}

// These are provider injection sinks, not every mention of a guide. A new
// producer in any Go package must enter the reviewed inventory. Task-message
// composers and guides are pinned separately through the catalog's sources.
func transparencyInjectionSources(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			found := false
			ast.Inspect(file, func(n ast.Node) bool {
				literal, ok := n.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(literal.Value)
				if err != nil {
					return true
				}
				for _, marker := range []string{"developer_instructions", "--append-system-prompt"} {
					if strings.Contains(text, marker) {
						found = true
					}
				}
				return true
			})
			if found {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				paths = append(paths, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func TestTransparencyMatchesReviewedSourcesAndBothLanguages(t *testing.T) {
	root := repoRoot(t)
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var catalog transparencyCatalog
	if err := json.Unmarshal(read("web/console/src/pages/transparency/catalog.json"), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Schema != 1 || len(catalog.Cards) == 0 || len(catalog.SourceChecks) == 0 {
		t.Fatal("transparency inventory is absent or unsupported")
	}
	digest := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
	for path, expected := range catalog.SourceChecks {
		if digest(read(path)) != expected {
			t.Errorf("%s changed: review the transparency explanation and both languages, then refresh its source receipt", path)
		}
	}
	for _, path := range transparencyInjectionSources(t, root) {
		if _, ok := catalog.SourceChecks[path]; !ok {
			t.Errorf("unregistered provider instruction producer: %s", path)
		}
	}
	seen := map[string]bool{}
	for _, card := range catalog.Cards {
		if seen[card.ID] || card.ID == "" {
			t.Errorf("duplicate or empty transparency id %q", card.ID)
		}
		seen[card.ID] = true
		if len(card.Sources) == 0 {
			t.Errorf("%s has no source", card.ID)
		}
		for _, source := range card.Sources {
			if _, ok := catalog.SourceChecks[source.Path]; !ok {
				t.Errorf("%s has unreviewed source %s", card.ID, source.Path)
			}
		}
	}
	for _, lang := range []string{"en", "zh-Hant"} {
		var words map[string]string
		if err := json.Unmarshal(read("web/console/public/catalogs/"+lang+".json"), &words); err != nil {
			t.Fatal(err)
		}
		reviewed := map[string]string{}
		for key, value := range words {
			if strings.HasPrefix(key, "transparency.") {
				reviewed[key] = value
			}
		}
		encoded, err := json.Marshal(reviewed)
		if err != nil {
			t.Fatal(err)
		}
		if digest(encoded) != catalog.CopyChecks[lang] {
			t.Errorf("%s transparency copy differs from its review receipt; verify both languages and refresh", lang)
		}
		for _, card := range catalog.Cards {
			for _, field := range []string{"title", "condition", "summary", "how", "effect"} {
				if strings.TrimSpace(words["transparency.card."+card.ID+"."+field]) == "" {
					t.Errorf("%s: %s lacks %s", lang, card.ID, field)
				}
			}
		}
	}
}

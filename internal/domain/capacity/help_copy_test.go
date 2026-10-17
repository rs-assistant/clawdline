package capacity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Settings page's (i) on a capacity row opens two sentences written for
// that row: what it counts and why it has a ceiling, and what a person notices
// when it is full. Before this guard, all but four rows shared one template
// that restated the row's name and the number already on screen; a person read
// it as saying nothing. A row registered without its own copy now fails here,
// in English and in Taiwan Traditional Chinese, and copy left behind by a
// removed row fails too.
func TestEveryRowExplainsItselfToAPerson(t *testing.T) {
	registered := map[string]bool{}
	for _, e := range Register() {
		registered[e.Name] = true
	}
	for _, lang := range []string{"en", "zh-Hant"} {
		path := filepath.Join("..", "..", "..", "web", "console", "public", "catalogs", lang+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(raw, &catalog); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for name := range registered {
			for _, part := range []string{"what", "full"} {
				key := "capacityRow." + name + "." + part
				if strings.TrimSpace(catalog[key]) == "" {
					t.Errorf("%s: %s has no %q copy (%s)", lang, name, part, key)
				}
			}
		}
		for key := range catalog {
			rest, ok := strings.CutPrefix(key, "capacityRow.")
			if !ok {
				continue
			}
			name := rest[:max(0, strings.LastIndex(rest, "."))]
			if !registered[name] {
				t.Errorf("%s: %s explains a row that is not registered", lang, key)
			}
		}
	}
}

package cloudvocab

import (
	"bytes"
	"strings"
	"testing"
)

func TestTheEmbeddedTableReads(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) < 100 {
		t.Fatalf("the table holds %d operations; the bridge has well over a hundred", len(ops))
	}
}

// A table that contradicts itself is refused whole, never read in part.
func TestATableThatContradictsItselfIsRefused(t *testing.T) {
	for name, edit := range map[string]func([]byte) []byte{
		"an unknown field": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"write_gate"`), []byte(`"write_gates"`), 1)
		},
		"a word twice": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"op": "archived-sessions"`), []byte(`"op": "archive-session"`), 1)
		},
		"a kind nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"kind": "read"`), []byte(`"kind": "peek"`), 1)
		},
		"another version": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 2`), 1)
		},
		"a check nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"checks": ["generation"]`), []byte(`"checks": ["generations"]`), 1)
		},
		"a special nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"special": ["content_read"]`), []byte(`"special": ["content_reads"]`), 1)
		},
		"a refusal special that is free text": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"refused:cloud_dispatch_unpinned"`), []byte(`"refused: no wire shape"`), 1)
		},
		"a rule for a field no shape names": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"decode": {"reply": "machine", "route": {"path": "/v1/sessions/archived"}}`),
				[]byte(`"decode": {"reply": "machine", "fields": {"ghost": {"type": "string"}}, "route": {"path": "/v1/sessions/archived"}}`), 1)
		},
		"a row with no decode": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"decode": {"reply": "machine", "route": {"path": "/v1/sessions/archived"}}`),
				[]byte(`"decode": null`), 1)
		},
		"a hand row that states a decode too": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"decode": {"hand": "its body is open`), []byte(`"decode": {"reply": "machine", "hand": "its body is open`), 1)
		},
		"a route path naming a field no shape names": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"route": {"path": "/v1/sessions/archived"}`), []byte(`"route": {"path": "/v1/sessions/{ghost}"}`), 1)
		},
		"a string rule on an integer": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"limit": {"type": "integer", "min": 1, "max": 1000}`),
				[]byte(`"limit": {"type": "integer", "min": 1, "max": 1000, "max_bytes": 4}`), 1)
		},
		"a wrapper check nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"strip_check": "generation"`), []byte(`"strip_check": "generations"`), 1)
		},
	} {
		edited := edit(vocabularyJSON)
		if bytes.Equal(edited, vocabularyJSON) {
			t.Fatalf("%s: the edit did not apply", name)
		}
		if _, err := parse(edited); err == nil || !strings.HasPrefix(err.Error(), "cloudvocab:") {
			t.Errorf("%s was read: %v", name, err)
		}
	}
}

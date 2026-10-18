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

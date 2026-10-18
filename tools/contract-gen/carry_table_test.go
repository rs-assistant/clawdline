package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

// The console's carry lists are the vocabulary table's, so a row added, moved
// to another class or reworded and not regenerated fails here by file, the
// same question `go run ./tools/contract-gen -check` asks of every output.
func TestTheConsoleCarryTableIsTheVocabularyTable(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	want, err := renderCarryTable(ops)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "web", "console", "src", "cloud", "carry-generated.ts")
	have, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(have, want) {
		t.Fatalf("%s is not what the vocabulary table says; run: go run ./tools/contract-gen", path)
	}
}

// Each list's words are exactly the table's rows of that class, and a
// deferred or unrouted word carries its sentence, not a route.
func TestTheCarryListsFollowEachRowsClass(t *testing.T) {
	ops := []cloudvocab.Operation{
		{Op: "a", Console: cloudvocab.Carried, ConsoleRoute: "GET /v1/a", ConsoleNote: "Why a is carried."},
		{Op: "b.c", Console: cloudvocab.Deferred, ConsoleSentence: "B is not asked for yet: do it on the machine."},
		{Op: "d", Console: cloudvocab.NoMachineRoute, ConsoleSentence: "D has nothing behind it on this machine."},
	}
	out, err := renderCarryTable(ops)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export const CARRIED = {\n  // Why a is carried.\n  a: \"GET /v1/a\",\n} as const\n",
		"export const DEFERRED = {\n  \"b.c\": \"B is not asked for yet: do it on the machine.\",\n} as const\n",
		"export const NO_MACHINE_ROUTE = {\n  d: \"D has nothing behind it on this machine.\",\n} as const\n",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the generated table does not hold:\n%s\nit is:\n%s", want, out)
		}
	}
	if _, err := renderCarryTable(ops[:2]); err == nil {
		t.Error("a table with no unrouted word was rendered; its list would type as never")
	}
}

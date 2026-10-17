package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// This copy used to answer empty for a row with no backend, so a row only the
// process table listed fell back to the whole reading and read missing while
// iTerm2 could not be asked. It is now the process table's, as everywhere else.
func TestSessionListRowWithNoBackendIsTheProcessTables(t *testing.T) {
	row := session.Session{ID: "row-1"}
	if got := rowSource(row); got != "ps" {
		t.Fatalf("a backendless row answered by %q, want ps", got)
	}
	inv := session.Inventory{Sources: map[string]bool{"ps": true, "iterm": false}}
	if !sourceCompleteRow(inv, row) {
		t.Fatal("a complete process table did not vouch for its own row")
	}
}

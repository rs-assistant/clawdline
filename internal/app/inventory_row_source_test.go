package app

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A row with no backend was listed by no terminal, so the inventory reading
// keeps putting it to the process table, as it did before the rule moved to
// session.OwningSource.
func TestInventoryRowWithNoBackendIsTheProcessTables(t *testing.T) {
	if got := rowSource(session.Session{ID: "ttys009", TTY: "ttys009"}); got != "ps" {
		t.Fatalf("a backendless tty row answered by %q, want ps", got)
	}
	if got := rowSource(session.Session{ID: "row-1"}); got != "ps" {
		t.Fatalf("a backendless row answered by %q, want ps", got)
	}
	if got := rowSource(session.Session{ID: "owned-1", Backend: session.BackendOwned}); got != "" {
		t.Fatalf("an owned pty answered by %q, want the whole reading", got)
	}
}

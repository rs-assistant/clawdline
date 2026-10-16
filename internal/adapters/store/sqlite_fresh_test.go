package store

import (
	"testing"
)

// A database Open creates is given its schema without a sync per statement;
// the handle it returns must write at synchronous=FULL like any other, and so
// must a reopened one.
func TestAFreshDatabaseIsBackAtFullSyncWhenOpenReturns(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"created", "reopened"} {
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var level int
		if err := s.db.QueryRow("PRAGMA synchronous").Scan(&level); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if level != 2 {
			t.Fatalf("%s: the write connection is at synchronous=%d, want 2 (FULL)", name, level)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

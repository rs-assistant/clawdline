package healthjournal

import (
	"path/filepath"
	"testing"
)

// A progress file outlives the store it read: the journal was moved aside
// and the new one has not written a segment yet. The old cursor is a typed
// invalid_cursor the uploader recovers from, never an out-of-range slice.
func TestAnOldCursorOnAnEmptyStoreIsInvalidNotAPanic(t *testing.T) {
	s := open(t, Options{Dir: filepath.Join(t.TempDir(), "journal"), BootID: "boot-new", Build: "abc"})
	for _, cursor := range []string{"v1.1.500", "v1.1.0", "v1.7.12"} {
		_, err := s.Read(Query{Cursor: cursor})
		if CodeOf(err) != CodeInvalidCursor {
			t.Fatalf("cursor %s on an empty store: want invalid_cursor, got %v", cursor, err)
		}
	}
}

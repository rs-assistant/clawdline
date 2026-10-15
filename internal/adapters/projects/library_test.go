package projects

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder in Dropbox where Claude Code was once run is not listed as a
// Project, so nothing reads it without the person choosing it: on 2026-10-15
// such a folder made macOS ask the person, on a reinstall, to let Clawdline
// read Dropbox. recorded is asked directly: on Linux the test's home is under
// /tmp, which the listing's own scratch rule drops whole.
func TestAFolderFoundUnderLibraryIsNotListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dropbox := filepath.Join(home, "Library", "CloudStorage", "Dropbox", "work")
	code := filepath.Join(home, "code", "app")
	for _, dir := range []string{dropbox, code} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		folder := filepath.Join(home, ".claude", "projects", Slug(dir))
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		row := `{"type":"user","cwd":"` + dir + `","message":{"role":"user","content":"hi"}}` + "\n"
		if err := os.WriteFile(filepath.Join(folder, "a.jsonl"), []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPlaces(func(string) string { return "" }, nil)
	found := map[string]bool{}
	for _, place := range p.recorded(60, 240) {
		found[place.Path] = true
	}
	if !found[code] || found[dropbox] {
		t.Fatalf("found %v; want %s and not %s", found, code, dropbox)
	}
	if !InUserLibrary(dropbox) || InUserLibrary(code) || InUserLibrary(filepath.Join(home, "LibraryNotes")) {
		t.Fatal("InUserLibrary does not tell ~/Library from its neighbours")
	}
}

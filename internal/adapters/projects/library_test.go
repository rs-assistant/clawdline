package projects

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder in Dropbox where Claude Code was once run is not listed as a
// Project, so nothing reads it without the person choosing it: on 2026-10-15
// such a folder made macOS ask the person, on a reinstall, to let Clawdline
// read Dropbox. A folder they registered is listed as before.
func TestAFolderFoundUnderLibraryIsNotListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The test's home is under the system's temporary directory, which the
	// listing treats as scratch; it is pointed elsewhere.
	t.Setenv("TMPDIR", t.TempDir())
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
	listed := map[string]bool{}
	for _, place := range p.List(nil, 10) {
		listed[place.Path] = true
	}
	if !listed[code] || listed[dropbox] {
		t.Fatalf("listed %v; want %s and not %s", listed, code, dropbox)
	}

	p.Registered = func() ([]RegisteredPlace, error) { return []RegisteredPlace{{Path: dropbox, AddedAt: 1}}, nil }
	listed = map[string]bool{}
	for _, place := range p.List(nil, 10) {
		listed[place.Path] = true
	}
	if !listed[dropbox] {
		t.Fatalf("a registered folder under ~/Library was dropped: %v", listed)
	}
}

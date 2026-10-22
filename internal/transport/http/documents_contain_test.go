package http

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

// A dispatched agent can write `artifacts -> elsewhere` into the directory it
// works in, so a project root stays inside its session's directory unless this
// daemon's own file says otherwise.
func TestAProjectRootStaysContainedUnlessTheFileSaysFalse(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !s.containsProjectRoot() {
		t.Fatal("no config.json followed the symlink")
	}
	for body, want := range map[string]bool{
		`{}`: true,
		`{"documents_contain_project_root": true}`:  true,
		`{"documents_contain_project_root": false}`: false,
		`{not json`: true,
	} {
		write(body)
		if got := s.containsProjectRoot(); got != want {
			t.Errorf("%s: contained=%t, want %t", body, got, want)
		}
	}
}

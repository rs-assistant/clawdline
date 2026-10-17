package install

import (
	"bytes"
	"os"
	"path/filepath"
)

// SameAppShell reports whether two bundles carry the same native shell.
// Service installations serve the console from the daemon, so their app
// bundle only needs replacing when this shell changes.
func SameAppShell(installed, incoming string) bool {
	marker := filepath.Join("Contents", "Resources", "shell-revision")
	a, err := os.ReadFile(filepath.Join(installed, marker))
	if err != nil || len(bytes.TrimSpace(a)) != 64 {
		return false
	}
	b, err := os.ReadFile(filepath.Join(incoming, marker))
	return err == nil && bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}

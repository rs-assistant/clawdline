package projects

import (
	"os"
	"path/filepath"
	"strings"
)

// InUserLibrary is whether path is under the person's ~/Library.
//
// Cloud-storage folders live there — Dropbox, Google Drive and OneDrive under
// ~/Library/CloudStorage, iCloud Drive under ~/Library/Mobile Documents — and
// macOS asks the person before a process reads one. A Clawdline process that
// stats or reads such a folder on its own makes that dialog name Clawdline:
// on 2026-10-15 a reinstall of the app asked for Dropbox access because a
// folder in Dropbox, where Claude Code had once been run, was listed as a
// Project and read in the background. So nothing reads there unless the
// person chose it: a folder they registered, or one a running session is in.
func InUserLibrary(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	library := filepath.Join(home, "Library") + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(path)+string(filepath.Separator), library)
}

package http

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxProjectFolderRows bounds one browser directory page. The browser can enter
// a path directly when a directory has more entries than this page can show.
const maxProjectFolderRows = 128

type projectFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// projectFoldersRoute lets a paired viewer choose a directory on the selected
// machine. It sends names only, never file contents, and skips hidden entries
// and links. Registration still validates the chosen path independently.
func (s *Server) projectFoldersRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	for key := range r.URL.Query() {
		if key != "path" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Only path is accepted.")
			return
		}
	}
	dir := r.URL.Query().Get("path")
	if dir == "" {
		var err error
		dir, err = os.UserHomeDir()
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "home_unavailable", "The home directory is unavailable.")
			return
		}
	}
	if !filepath.IsAbs(dir) || len(dir) > 4096 || strings.IndexByte(dir, 0) >= 0 {
		writeRefusal(w, http.StatusBadRequest, "invalid_project_path", "Choose an absolute directory on this machine.")
		return
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeRefusal(w, http.StatusNotFound, "project_folder_unavailable", "This directory is unavailable on the selected machine.")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeRefusal(w, http.StatusForbidden, "project_folder_unreadable", "This directory cannot be read on the selected machine.")
		return
	}
	rows := make([]projectFolder, 0)
	truncated := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		if len(rows) == int(CapacityLimit("projects.folder_rows")) {
			truncated = true
			break
		}
		rows = append(rows, projectFolder{Name: entry.Name(), Path: filepath.Join(dir, entry.Name())})
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		parent = ""
	}
	writeJSON(w, struct {
		Path      string          `json:"path"`
		Parent    string          `json:"parent,omitempty"`
		Folders   []projectFolder `json:"folders"`
		Truncated bool            `json:"truncated"`
	}{dir, parent, rows, truncated})
}

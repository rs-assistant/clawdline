package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestProjectAndSessionIconReadLiveOverride(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	path := filepath.Join(home, "projects", "wave")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.OpenPlaceRegistry(state).Add([]string{path}, time.Now()); err != nil {
		t.Fatal(err)
	}
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: state}, broker: &orchestrator.Broker{Dir: state}, icons: icons}
	read := func() (string, string, int) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.placesRoute(rec, httptest.NewRequest("GET", "/v1/places", nil))
		if rec.Code != 200 {
			return "", "", rec.Code
		}
		var answer struct {
			Places []struct {
				Path string `json:"path"`
				Icon struct {
					Accent string `json:"accent"`
				} `json:"icon"`
				Setup struct {
					Icon string `json:"icon"`
				} `json:"setup"`
			} `json:"places"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		for _, p := range answer.Places {
			if p.Path == path {
				return p.Icon.Accent, p.Setup.Icon, rec.Code
			}
		}
		t.Fatal("project absent")
		return "", "", 0
	}
	file := filepath.Join(state, "project-icon-overrides.json")
	write := func(color string) {
		t.Helper()
		data, err := json.Marshal(map[string]icon.Grid{path: {Accent: color, Cells: [][]*string{{&color}}}})
		if err != nil {
			t.Fatal(err)
		}
		tmp := file + ".new"
		if err := os.WriteFile(tmp, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, file); err != nil {
			t.Fatal(err)
		}
	}
	write("#68C8D8")
	if accent, source, code := read(); accent != "#68C8D8" || source != "override" || code != 200 {
		t.Fatalf("created: %s %s %d", accent, source, code)
	}
	row := s.sessionRow(rowInput{item: session.Session{CWD: path}})
	if row.Icon == nil || row.Icon.Accent != "#68C8D8" {
		t.Fatalf("session icon: %+v", row.Icon)
	}
	write("#123456")
	if accent, source, code := read(); accent != "#123456" || source != "override" || code != 200 {
		t.Fatalf("replaced: %s %s %d", accent, source, code)
	}
	row = s.sessionRow(rowInput{item: session.Session{CWD: path}})
	if row.Icon == nil || row.Icon.Accent != "#123456" {
		t.Fatalf("session replacement: %+v", row.Icon)
	}
	stale := sessionsSnapshotWire{Sessions: []sessionRowWire{{SessionRow: row.SessionRow}}}
	stale.Sessions[0].Icon.Accent = "#000000"
	if err := s.refreshSnapshotIcons(&stale); err != nil {
		t.Fatal(err)
	}
	if stale.Sessions[0].Icon.Accent != "#123456" {
		t.Fatalf("held Session snapshot: %+v", stale.Sessions[0].Icon)
	}
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := read(); code != 503 {
		t.Fatalf("corrupt override returned %d", code)
	}
	if err := s.refreshSnapshotIcons(&stale); err == nil {
		t.Fatal("corrupt override refreshed Session snapshot")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, source, code := read(); source == "override" || code != 200 {
		t.Fatalf("removed: %s %d", source, code)
	}
	// The compatibility source also refuses a damaged refresh.
	legacy := filepath.Join(home, ".claude", "project-icons.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := read(); code != 503 {
		t.Fatalf("corrupt legacy returned %d", code)
	}
}

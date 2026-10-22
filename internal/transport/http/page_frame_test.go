package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// No page elsewhere may frame the console, its document or its bundle.
func TestTheConsoleRefusesToBeFramed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(`<html lang="en"><head></head><body></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bar.html"), []byte(`<html></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/bar.html"} {
		rec := httptest.NewRecorder()
		newPage(root).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		h := rec.Header()
		if h.Get("Content-Security-Policy") != "frame-ancestors 'none'" || h.Get("X-Frame-Options") != "DENY" ||
			h.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: framing headers %v", path, h)
		}
	}
}

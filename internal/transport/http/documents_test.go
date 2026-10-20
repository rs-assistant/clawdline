package http

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTMLDocumentResponseIsSanitizedAndSandboxed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.html"), []byte(`<meta http-equiv="refresh" content="0;url=https://evil.invalid"><script>alert(1)</script><details><summary>Report</summary>safe</details>`), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	(&Server{}).serveDocument(w, root, "report.html")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("response: %d %v", w.Code, w.Header())
	}
	if w.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'; script-src 'none'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'" {
		t.Fatal(w.Header())
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Header())
	}
	if strings.Contains(w.Body.String(), "evil.invalid") || strings.Contains(w.Body.String(), "<script") || !strings.Contains(w.Body.String(), "<details>") {
		t.Fatal(w.Body.String())
	}
}

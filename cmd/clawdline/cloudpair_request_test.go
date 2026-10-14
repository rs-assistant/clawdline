package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

func TestPairRequestUsesVerifiedInstalledDaemonWhenShellStateHasNoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cloud/status" || r.Header.Get("Authorization") != "Bearer installed-token" {
			t.Errorf("verification request: %s %q", r.URL.Path, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	installed := t.TempDir()
	if err := os.WriteFile(filepath.Join(installed, "local-token"), []byte("installed-token"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := config.Config{Dir: t.TempDir()}
	staleListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stalePort := staleListener.Addr().(*net.TCPAddr).Port
	staleListener.Close()
	req, err := cloudPairRequestFor(http.MethodPost, "/v1/cloud/pairing", nil, stale, installed, stalePort, port)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/v1/cloud/pairing" || req.Header.Get("Authorization") != "Bearer installed-token" {
		t.Fatalf("pair request: %s %q", req.URL.Path, req.Header.Get("Authorization"))
	}
	selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer selected.Close()
	_, selectedPortText, err := net.SplitHostPort(strings.TrimPrefix(selected.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	selectedPort, err := strconv.Atoi(selectedPortText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cloudPairRequestFor(http.MethodPost, "/v1/cloud/pairing", nil, stale, installed, selectedPort, port); err == nil {
		t.Fatal("a reachable selected daemon must not silently select the installed daemon")
	}
}

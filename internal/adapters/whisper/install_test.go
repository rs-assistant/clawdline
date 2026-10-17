package whisper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelInstallIsVerifiedBeforeItAppears(t *testing.T) {
	body := []byte("a complete tiny model")
	sum := sha256.Sum256(body)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	was := DefaultModel
	DefaultModel = Model{Name: "tiny", File: "ggml-tiny.bin", URL: server.URL, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	t.Cleanup(func() { DefaultModel = was })

	home := t.TempDir()
	path, err := InstallDefaultModel(context.Background(), server.Client(), home)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".cache", "whisper", "ggml-tiny.bin") {
		t.Fatalf("path %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("installed %q: %v", got, err)
	}
	parts, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.part"))
	if len(parts) != 0 {
		t.Fatalf("partial files remain: %v", parts)
	}
}

func TestBadModelNeverReplacesTheTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("wrong"))
	}))
	defer server.Close()
	was := DefaultModel
	DefaultModel = Model{Name: "tiny", File: "ggml-tiny.bin", URL: server.URL, Size: 5, SHA256: strings.Repeat("0", 64)}
	t.Cleanup(func() { DefaultModel = was })
	home := t.TempDir()
	if _, err := InstallDefaultModel(context.Background(), server.Client(), home); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "whisper", "ggml-tiny.bin")); !os.IsNotExist(err) {
		t.Fatalf("bad model appeared: %v", err)
	}
}

func TestBinaryInstallCommandDoesNotSuggestTheIncompatiblePythonPackage(t *testing.T) {
	for _, c := range []struct{ goos, pm, want string }{
		{"darwin", "brew", "brew install whisper-cpp"},
		{"linux", "pacman", "sudo pacman -S --needed whisper-cpp"},
		{"linux", "apt-get", "git clone --depth 1"},
	} {
		got := BinaryInstallCommand(c.goos, c.pm)
		if !strings.Contains(got, c.want) || strings.Contains(got, "pip") || strings.Contains(got, "openai-whisper") {
			t.Errorf("%s/%s: %q", c.goos, c.pm, got)
		}
	}
}

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

func TestVoiceSetupSaysWhichHalfIsMissingAndWhereItLooked(t *testing.T) {
	dir := t.TempDir()
	binDir, modelDir := filepath.Join(dir, "bin"), filepath.Join(dir, "models")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "whisper-cli")
	if err := os.WriteFile(binary, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	setup := voiceSetup(whisper.Transcriber{BinaryDirs: []string{binDir}, ModelDirs: []string{modelDir}})
	if setup.Status != "no_model" || setup.Binary != "whisper-cli" || setup.Model != "" {
		t.Fatalf("setup: %+v", setup)
	}
	if len(setup.BinaryDirs) != 1 || setup.BinaryDirs[0] != binDir || len(setup.ModelDirs) != 1 || setup.ModelDirs[0] != modelDir {
		t.Fatalf("searched paths: %+v", setup)
	}
	if setup.ModelName != whisper.DefaultModel.Name || setup.ModelBytes != whisper.DefaultModel.Size {
		t.Fatalf("offered model: %+v", setup)
	}
}

func TestVoiceSetupInstallsThePinnedModel(t *testing.T) {
	was := voiceModelInstall
	voiceModelInstall = func(context.Context, *http.Client, string) (string, error) {
		return "/private/home/.cache/whisper/ggml-test.bin", nil
	}
	t.Cleanup(func() { voiceModelInstall = was })

	rec := httptest.NewRecorder()
	voiceServer(t).voiceSetupRoute(rec, httptest.NewRequest(http.MethodPost, "/v1/voice/setup", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body)
	}
	var got voiceModelInstallReply
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.OK || got.Model != "ggml-test.bin" {
		t.Fatalf("answer: %+v, %v", got, err)
	}
}

func TestVoiceSetupHidesTheHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := privatePaths([]string{filepath.Join(home, ".cache", "whisper"), "/usr/share/whisper"})
	if len(got) != 2 || got[0] != "~/.cache/whisper" || got[1] != "/usr/share/whisper" {
		t.Fatalf("paths: %v", got)
	}
}

package whisper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// DefaultModel is the one model Clawdline offers to install. The URL is pinned
// to the upstream commit that introduced the file; Size and SHA256 are the LFS
// object's own metadata, so an error page or a changed object never becomes a
// model merely because it has a .bin name.
var DefaultModel = Model{
	Name:   "large-v3-turbo-q5_0",
	File:   "ggml-large-v3-turbo-q5_0.bin",
	URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/98aa99a0a9db05ae2342309f5096248665f7cba3/ggml-large-v3-turbo-q5_0.bin",
	Size:   574041195,
	SHA256: "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2",
}

// ModelDownloadBytesLimit is one installed model plus no extra byte. It is a
// registered capacity bound: a response beyond it is refused and the partial
// file is removed.
const ModelDownloadBytesLimit = 574041195

// ErrInstallBusy means another request is already downloading the shared model.
var ErrInstallBusy = errors.New("a whisper model is already being downloaded")

type Model struct {
	Name   string
	File   string
	URL    string
	Size   int64
	SHA256 string
}

var modelInstall sync.Mutex

// InstallDefaultModel downloads and verifies the offered model into the first
// search directory. A completed file is renamed into place only after its size
// and digest agree with the pinned upstream object.
func InstallDefaultModel(ctx context.Context, client *http.Client, home string) (string, error) {
	if !modelInstall.TryLock() {
		return "", ErrInstallBusy
	}
	defer modelInstall.Unlock()
	if client == nil {
		client = http.DefaultClient
	}
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("find home directory: %w", err)
		}
	}
	dir := filepath.Join(home, ".cache", "whisper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make model directory: %w", err)
	}
	target := filepath.Join(dir, DefaultModel.File)
	if ok, err := verifiedModel(target, DefaultModel); err != nil {
		return "", err
	} else if ok {
		return target, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultModel.URL, nil)
	if err != nil {
		return "", fmt.Errorf("make model request: %w", err)
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download model: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download model: upstream answered %s", res.Status)
	}
	if res.ContentLength > ModelDownloadBytesLimit {
		return "", fmt.Errorf("download model: upstream declared %d bytes, limit is %d", res.ContentLength, ModelDownloadBytesLimit)
	}

	tmp, err := os.CreateTemp(dir, "."+DefaultModel.File+".*.part")
	if err != nil {
		return "", fmt.Errorf("create partial model: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("protect partial model: %w", err)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(res.Body, ModelDownloadBytesLimit+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("download model: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("finish model: %w", closeErr)
	}
	if n != DefaultModel.Size {
		return "", fmt.Errorf("download model: got %d bytes, want %d", n, DefaultModel.Size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, DefaultModel.SHA256) {
		return "", fmt.Errorf("download model: sha256 %s does not match upstream", got)
	}
	if err := os.Rename(name, target); err != nil {
		return "", fmt.Errorf("install model: %w", err)
	}
	return target, nil
}

func verifiedModel(path string, model Model) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open existing model: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() != model.Size {
		return false, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("verify existing model: %w", err)
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), model.SHA256), nil
}

// BinaryInstallCommand is executable guidance for the package manager whose
// package is known to contain whisper-cli. Other Linux distributions get the
// upstream build because similarly named Python and Fedora library packages do
// not provide the command Clawdline runs.
func BinaryInstallCommand(goos, packageManager string) string {
	switch {
	case goos == "darwin":
		return "brew install whisper-cpp"
	case goos == "linux" && packageManager == "pacman":
		return "sudo pacman -S --needed whisper-cpp"
	case goos == "windows":
		return "Download the current whisper.cpp Windows archive from https://github.com/ggml-org/whisper.cpp/releases"
	default:
		return "git clone --depth 1 https://github.com/ggml-org/whisper.cpp ~/whisper.cpp\ncmake -S ~/whisper.cpp -B ~/whisper.cpp/build -DCMAKE_BUILD_TYPE=Release\ncmake --build ~/whisper.cpp/build --config Release -j"
	}
}

// SearchPaths returns the exact places Ready checks, without exposing mutable
// slices owned by the transcriber.
func (t Transcriber) SearchPaths() (binary, model []string) {
	return append([]string(nil), t.binaryDirs()...), append([]string(nil), t.modelDirs()...)
}

// Platform is kept as a function so tests of the setup response do not need to
// infer which build tag selected this file.
func Platform() string { return runtime.GOOS }

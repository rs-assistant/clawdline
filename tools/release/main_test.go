package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A release signed here carries both signatures — manifest.sig.json for a
// daemon, manifest.sshsig for install.sh — and verify refuses one without
// the second, or a manifest with no committed_at to tell its age by.
func TestASignedReleaseCarriesBothSignaturesAndItsTime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clawdline_v1.0.0_linux_amd64.tar.gz"), []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	if err := manifest([]string{"-version", "v1.0.0", "-commit", commit, "-base-url", "https://example.invalid/v1.0.0", dir}); err == nil {
		t.Fatal("a manifest with no -committed-at was built")
	}
	if err := manifest([]string{"-version", "v1.0.0", "-commit", commit, "-committed-at", "2026-10-22T00:00:00Z",
		"-base-url", "https://example.invalid/v1.0.0", dir}); err != nil {
		t.Fatal(err)
	}
	if err := sign([]string{"-key", keyFile, dir}); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(pub)
	if err := verify([]string{"-pub", b64, dir}); err != nil {
		t.Fatalf("a release signed both ways: %v", err)
	}
	ssh := filepath.Join(dir, "manifest.sshsig")
	good, err := os.ReadFile(ssh)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ssh); err != nil {
		t.Fatal(err)
	}
	if err := verify([]string{"-pub", b64, dir}); err == nil {
		t.Fatal("a release with no manifest.sshsig verified")
	}
	_, other, _ := ed25519.GenerateKey(nil)
	otherFile := filepath.Join(t.TempDir(), "other")
	_ = os.WriteFile(otherFile, []byte(base64.StdEncoding.EncodeToString(other.Seed())), 0o600)
	_ = os.WriteFile(ssh, good, 0o644)
	// Another key's SSH signature over the same manifest is not this key's.
	sigs, _ := os.ReadFile(filepath.Join(dir, "manifest.sig.json"))
	if err := sign([]string{"-key", otherFile, dir}); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "manifest.sig.json"), sigs, 0o644)
	if err := verify([]string{"-pub", b64, dir}); err == nil {
		t.Fatal("a manifest.sshsig by an untrusted key verified")
	}
}

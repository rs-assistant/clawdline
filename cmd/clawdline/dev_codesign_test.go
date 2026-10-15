package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A dev build is signed with the machine's certificate only when both of its
// files are there; otherwise it is signed ad hoc.
func TestADevBuildFindsTheMachinesCertificate(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("dev builds are signed on macOS only")
	}
	dir := t.TempDir()
	if _, _, ok := devCodesign(dir); ok {
		t.Fatal("found a certificate in an empty directory")
	}
	signing := filepath.Join(dir, "signing")
	if err := os.MkdirAll(signing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(signing, "codesign.p12"), []byte("p12"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := devCodesign(dir); ok {
		t.Fatal("found a certificate without its password")
	}
	if err := os.WriteFile(filepath.Join(signing, "codesign.password"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p12, password, ok := devCodesign(dir)
	if !ok || p12 != filepath.Join(signing, "codesign.p12") || password != "secret" {
		t.Fatalf("devCodesign = %q, %q, %v", p12, password, ok)
	}
}

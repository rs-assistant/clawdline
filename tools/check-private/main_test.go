package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestReleaseLicenseEmailExceptionNeedsExactPathAndBytes(t *testing.T) {
	t.Parallel()
	license := []byte("Public upstream license with authors' addresses\n")
	sum := sha256.Sum256(license)
	digest := hex.EncodeToString(sum[:])
	path := "clawdline_v0.11.0_linux_amd64/licenses/libevent.txt"
	if !exactLibeventLicense(path, license, digest) {
		t.Fatal("the exact license under an artifact must be recognized")
	}
	if exactLibeventLicense(path, append(append([]byte(nil), license...), 'x'), digest) {
		t.Fatal("a changed license must lose the exception")
	}
	if exactLibeventLicense("clawdline_v0.11.0_linux_amd64/docs/libevent.txt", license, digest) {
		t.Fatal("the same bytes outside the license path must not be recognized")
	}
	if exactLibeventLicense("licenses/libevent.txt", license, digest) {
		t.Fatal("a repository file is not a release artifact license")
	}
}

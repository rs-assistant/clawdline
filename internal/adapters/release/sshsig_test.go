package release

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The SSH copy of a release signature verifies with the key that made it and
// with nothing else, over the manifest's exact bytes.
func TestTheSSHSignatureVerifiesOnlyItsManifestAndKey(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	other, _, _ := ed25519.GenerateKey(nil)
	manifest := []byte(`{"version": "v1.0.0"}` + "\n")
	sig := SignSSH(manifest, priv)
	if err := VerifySSH(manifest, sig, []ed25519.PublicKey{other, pub}); err != nil {
		t.Fatalf("a good SSH signature: %v", err)
	}
	for name, c := range map[string]struct {
		manifest, sig []byte
		keys          []ed25519.PublicKey
	}{
		"tampered manifest": {bytes.Replace(manifest, []byte("1.0.0"), []byte("1.0.1"), 1), sig, []ed25519.PublicKey{pub}},
		"untrusted key":     {manifest, sig, []ed25519.PublicKey{other}},
		"no armor":          {manifest, []byte("AAAA"), []ed25519.PublicKey{pub}},
		"truncated":         {manifest, append(append([]byte{}, sig[:60]...), []byte("\n-----END SSH SIGNATURE-----\n")...), []ed25519.PublicKey{pub}},
	} {
		if err := VerifySSH(c.manifest, c.sig, c.keys); err == nil {
			t.Errorf("%s verified", name)
		}
	}
}

// OpenSSH's own ssh-keygen accepts the signature, which is what install.sh
// runs on a Mac whose openssl cannot check Ed25519. Skipped where there is
// no ssh-keygen; macOS always has one. RELEASE_TEST_SSH_KEYGEN names a
// binary to use instead of the one on PATH.
func TestSSHKeygenVerifiesTheSSHSignature(t *testing.T) {
	keygen := os.Getenv("RELEASE_TEST_SSH_KEYGEN")
	if keygen == "" {
		var err error
		if keygen, err = exec.LookPath("ssh-keygen"); err != nil {
			t.Skip("no ssh-keygen on this machine")
		}
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	dir := t.TempDir()
	manifest := []byte("{\n  \"version\": \"v1.0.0\"\n}\n")
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mpath := write("manifest.json", manifest)
	spath := write(SSHSignatureFile, SignSSH(manifest, priv))
	signers := write("allowed_signers", []byte("clawdline-release "+SSHPublicKey(pub)+"\n"))
	verify := func(message string) error {
		f, err := os.Open(message)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cmd := exec.Command(keygen, "-Y", "verify", "-f", signers, "-I", "clawdline-release", "-n", SSHNamespace, "-s", spath)
		cmd.Stdin = f
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("ssh-keygen: %s", strings.TrimSpace(string(out)))
		}
		return err
	}
	if err := verify(mpath); err != nil {
		t.Fatalf("ssh-keygen refused a good signature: %v", err)
	}
	if err := verify(write("other.json", []byte("{\n  \"version\": \"v1.0.1\"\n}\n"))); err == nil {
		t.Fatal("ssh-keygen accepted the signature over another manifest")
	}
	// And the other way: a signature ssh-keygen made verifies here.
	keyPath := filepath.Join(dir, "id")
	if out, err := exec.Command(keygen, "-q", "-t", "ed25519", "-N", "", "-f", keyPath).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -t ed25519: %v %s", err, out)
	}
	cmd := exec.Command(keygen, "-Y", "sign", "-f", keyPath, "-n", SSHNamespace, mpath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -Y sign: %v %s", err, out)
	}
	theirs, err := os.ReadFile(mpath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	pubLine, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(pubLine))
	if len(fields) < 2 {
		t.Fatalf("ssh-keygen wrote %q", pubLine)
	}
	theirKey := keyFromSSH(t, fields[0]+" "+fields[1])
	if err := VerifySSH(manifest, theirs, []ed25519.PublicKey{theirKey}); err != nil {
		t.Fatalf("an ssh-keygen signature does not verify here: %v", err)
	}
}

// keyFromSSH reverses SSHPublicKey.
func keyFromSSH(t *testing.T, line string) ed25519.PublicKey {
	t.Helper()
	parts := strings.Fields(line)
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	r := &sshReader{b: raw}
	typ, key := r.str(), r.str()
	if r.bad || string(typ) != sshKeyType || len(key) != ed25519.PublicKeySize {
		t.Fatalf("%q is not an ssh-ed25519 key", line)
	}
	if SSHPublicKey(key) != parts[0]+" "+parts[1] {
		t.Fatalf("SSHPublicKey does not give back %q", line)
	}
	return ed25519.PublicKey(key)
}

// A manifest answers "is there something newer" only while its signed
// committed_at is within the bound; one without it is refused, because an
// old manifest replayed on purpose would be chosen for lacking it.
func TestOnlyARecentManifestIsTheNewestRelease(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	m := Manifest{Version: "v1.0.0", CommittedAt: now.Add(-MaxManifestAge() + time.Hour).Format(time.RFC3339)}
	if err := m.Fresh(now); err != nil {
		t.Fatalf("a manifest inside the bound: %v", err)
	}
	for _, at := range []string{now.Add(-MaxManifestAge() - time.Hour).Format(time.RFC3339), "", "yesterday"} {
		m.CommittedAt = at
		if e, ok := m.Fresh(now).(*Error); !ok || e.Code != CodeManifestStale {
			t.Errorf("committed_at %q: %v, want %s", at, m.Fresh(now), CodeManifestStale)
		}
	}
}

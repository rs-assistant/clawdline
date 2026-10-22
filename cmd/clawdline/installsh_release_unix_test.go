//go:build !windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
)

// installRelease is a release install.sh downloads from a local server,
// signed with a key of its own that the run trusts through
// CLAWDLINE_INSTALL_TEST_KEY. Its `clawdline` is a script that only records
// that it ran, so a refusal can be proved to come before anything
// downloaded was executed.
type installRelease struct {
	t       *testing.T
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
	srv     *httptest.Server
	mu      sync.Mutex
	files   map[string][]byte
	home    string
	root    string
	marker  string
	version string
}

func newInstallRelease(t *testing.T, version string, committedAt time.Time) *installRelease {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &installRelease{t: t, pub: pub, priv: priv, files: map[string][]byte{}, version: version}
	r.home = t.TempDir()
	r.root = filepath.Join(r.home, "root")
	r.marker = filepath.Join(r.home, "ran")
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		body, ok := r.files[strings.TrimPrefix(req.URL.Path, "/")]
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(r.srv.Close)
	r.publish(committedAt, nil)
	return r
}

// publish builds the archive and a manifest as tools/release writes it
// (indented, one field to a line), and signs it both ways.
func (r *installRelease) publish(committedAt time.Time, edit func(*release.Manifest)) {
	r.t.Helper()
	archive := fakeReleaseArchive(r.t, "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$CLAWDLINE_TEST_MARKER\"\n")
	name := "clawdline_" + r.version + "_" + osArch() + ".tar.gz"
	sum := sha256.Sum256(archive)
	m := release.Manifest{Version: r.version, Commit: strings.Repeat("ab", 20), CommittedAt: committedAt.UTC().Format(time.RFC3339),
		Channel: "stable", Artifacts: []release.Artifact{
			{OS: "darwin", Arch: "zz", Kind: release.KindDaemon, Name: "clawdline_" + r.version + "_darwin_zz.tar.gz",
				URL: r.srv.URL + "/other", Size: 1, SHA256: strings.Repeat("0", 64)},
			{OS: hostOS(), Arch: hostArch(), Kind: release.KindDaemon, Name: name, URL: r.srv.URL + "/" + name,
				Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:])},
		}}
	if edit != nil {
		edit(&m)
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		r.t.Fatal(err)
	}
	mb = append(mb, '\n')
	sb, _ := json.MarshalIndent([]release.Signature{release.Sign(mb, r.priv)}, "", "  ")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files = map[string][]byte{
		"manifest.json":          mb,
		"manifest.sig.json":      append(sb, '\n'),
		release.SSHSignatureFile: release.SignSSH(mb, r.priv),
		name:                     archive,
		// Unsigned, as every release has it; install.sh no longer reads it.
		"SHA256SUMS": []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"),
	}
}

func (r *installRelease) set(name string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if body == nil {
		delete(r.files, name)
		return
	}
	r.files[name] = body
}

func (r *installRelease) get(name string) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte{}, r.files[name]...)
}

func hostOS() string {
	if out, _ := exec.Command("uname", "-s").Output(); strings.TrimSpace(string(out)) == "Darwin" {
		return "darwin"
	}
	return "linux"
}

func hostArch() string {
	out, _ := exec.Command("uname", "-m").Output()
	switch strings.TrimSpace(string(out)) {
	case "arm64", "aarch64":
		return "arm64"
	}
	if hostOS() == "darwin" {
		if out, _ := exec.Command("sysctl", "-n", "hw.optional.arm64").Output(); strings.TrimSpace(string(out)) == "1" {
			return "arm64"
		}
	}
	return "amd64"
}

func osArch() string { return hostOS() + "_" + hostArch() }

func fakeReleaseArchive(t *testing.T, script string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range []struct {
		name, body string
		dir        bool
	}{{name: "clawdline", body: script}, {name: "dist/", dir: true}, {name: "dist/BUILD.json", body: "{}\n"}} {
		h := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(e.body))}
		if e.dir {
			h.Typeflag, h.Size = tar.TypeDir, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// toolDir is a PATH holding only the commands install.sh may use, so a run
// can be given openssl or ssh-keygen, or neither, whatever this machine has.
func toolDir(t *testing.T, sshKeygen string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"sh", "curl", "tar", "gzip", "awk", "sed", "tr", "wc", "head", "cut", "mktemp", "mkdir",
		"rm", "rmdir", "mv", "cat", "grep", "readlink", "date", "uname", "sysctl", "shasum", "sha256sum", "base64", "find", "printf"} {
		if p, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if sshKeygen != "" {
		if err := os.Symlink(sshKeygen, filepath.Join(dir, "ssh-keygen")); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type installRun struct {
	out    string
	err    error
	ran    bool
	marker string
	// unpacked is what the run left in releases/ that was not there before.
	unpacked []string
}

func (r *installRelease) releases() map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir(filepath.Join(r.root, "releases"))
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// run is install.sh with args against the release. openssl names the
// OpenSSL it may use ("" none); sshKeygen the ssh-keygen ("" none).
func (r *installRelease) run(openssl, sshKeygen string, args ...string) installRun {
	r.t.Helper()
	os.Remove(r.marker)
	before := r.releases()
	cmd := exec.Command("sh", append([]string{"../../install.sh"}, args...)...)
	if openssl == "" {
		openssl = "/nonexistent/openssl"
	}
	cmd.Env = []string{
		"HOME=" + r.home, "PATH=" + toolDir(r.t, sshKeygen), "LC_ALL=C",
		"CLAWDLINE_NEXT_INSTALL_ROOT=" + r.root,
		"CLAWDLINE_INSTALL_BASE_URL=" + r.srv.URL,
		"CLAWDLINE_INSTALL_TEST_KEY=" + base64.StdEncoding.EncodeToString(r.pub),
		"CLAWDLINE_INSTALL_OPENSSL=" + openssl,
		"CLAWDLINE_TEST_MARKER=" + r.marker,
	}
	out, err := cmd.CombinedOutput()
	res := installRun{out: string(out), err: err}
	for name := range r.releases() {
		if !before[name] {
			res.unpacked = append(res.unpacked, name)
		}
	}
	if b, rerr := os.ReadFile(r.marker); rerr == nil {
		res.ran, res.marker = true, string(b)
	}
	return res
}

// refused requires a run that stopped with want in its message, before the
// downloaded binary ran and with no release left unpacked.
func (r *installRelease) refused(res installRun, want string) {
	r.t.Helper()
	if res.ran {
		r.t.Fatalf("the downloaded binary ran (%q); want a refusal %q before it:\n%s", strings.TrimSpace(res.marker), want, res.out)
	}
	if res.err == nil {
		r.t.Fatalf("install.sh succeeded; want a refusal %q:\n%s", want, res.out)
	}
	if !strings.Contains(res.out, want) {
		r.t.Fatalf("refusal does not say %q:\n%s", want, res.out)
	}
	if len(res.unpacked) > 0 {
		r.t.Fatalf("a refused install left %v unpacked:\n%s", res.unpacked, res.out)
	}
}

func (r *installRelease) installed(res installRun) {
	r.t.Helper()
	if res.err != nil || !res.ran {
		r.t.Fatalf("install.sh did not reach setup (err %v, ran %v):\n%s", res.err, res.ran, res.out)
	}
	if !strings.HasPrefix(res.marker, "setup --archive ") || !strings.Contains(res.out, "release signature checked") {
		r.t.Fatalf("setup ran as %q:\n%s", res.marker, res.out)
	}
}

func opensslWithEd25519(t *testing.T) string {
	t.Helper()
	p := os.Getenv("RELEASE_TEST_OPENSSL")
	if p == "" {
		var err error
		if p, err = exec.LookPath("openssl"); err != nil {
			return ""
		}
	}
	out, _ := exec.Command(p, "version").Output()
	if !strings.HasPrefix(string(out), "OpenSSL 3") {
		return ""
	}
	return p
}

func sshKeygenWithSign(t *testing.T) string {
	t.Helper()
	p := os.Getenv("RELEASE_TEST_SSH_KEYGEN")
	if p == "" {
		var err error
		if p, err = exec.LookPath("ssh-keygen"); err != nil {
			return ""
		}
	}
	return p
}

// install.sh checks the manifest's signature with openssl and the release
// keys it carries before it runs anything it downloaded: a good release
// reaches setup; a manifest changed after signing, a signature by another
// key, an archive that is not the one the manifest names, or a machine with
// nothing that can check Ed25519 stops it first.
func TestInstallScriptChecksTheSignatureBeforeRunningTheRelease(t *testing.T) {
	openssl := opensslWithEd25519(t)
	if openssl == "" {
		t.Skip("no OpenSSL 3 here (RELEASE_TEST_OPENSSL names one)")
	}
	now := time.Now()
	r := newInstallRelease(t, "v1.4.0", now)
	t.Run("good release", func(t *testing.T) {
		r.t = t
		r.installed(r.run(openssl, ""))
	})
	t.Run("tampered manifest", func(t *testing.T) {
		r.t = t
		good := r.get("manifest.json")
		r.set("manifest.json", bytes.Replace(good, []byte(`"channel": "stable"`), []byte(`"channel": "beta"`), 1))
		defer r.set("manifest.json", good)
		r.refused(r.run(openssl, ""), "is not signed by a Clawdline release key")
	})
	t.Run("signature by another key", func(t *testing.T) {
		r.t = t
		good := r.get("manifest.sig.json")
		defer r.set("manifest.sig.json", good)
		_, other, _ := ed25519.GenerateKey(nil)
		sb, _ := json.Marshal([]release.Signature{release.Sign(r.get("manifest.json"), other)})
		r.set("manifest.sig.json", sb)
		r.refused(r.run(openssl, ""), "is not signed by a Clawdline release key")
		r.set("manifest.sig.json", []byte("[]"))
		r.refused(r.run(openssl, ""), "is not signed by a Clawdline release key")
	})
	t.Run("archive not the signed one", func(t *testing.T) {
		r.t = t
		name := "clawdline_v1.4.0_" + osArch() + ".tar.gz"
		good := r.get(name)
		defer r.set(name, good)
		r.set(name, fakeReleaseArchive(t, "#!/bin/sh\nprintf evil >\"$CLAWDLINE_TEST_MARKER\"\n"))
		r.refused(r.run(openssl, ""), "the signed manifest says")
	})
	t.Run("no verifier", func(t *testing.T) {
		r.t = t
		r.refused(r.run("", ""), "nothing here can check this release's Ed25519 signature")
	})
	t.Run("the test key needs the test host", func(t *testing.T) {
		r.t = t
		// No curl, and a proxy that answers nothing: a script that went on
		// to the real release host could not reach it.
		path := toolDir(t, "")
		os.Remove(filepath.Join(path, "curl"))
		cmd := exec.Command("sh", "../../install.sh")
		cmd.Env = []string{"HOME=" + r.home, "PATH=" + path, "CLAWDLINE_NEXT_INSTALL_ROOT=" + r.root,
			"https_proxy=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9",
			"CLAWDLINE_INSTALL_TEST_KEY=" + base64.StdEncoding.EncodeToString(r.pub)}
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "honoured only with CLAWDLINE_INSTALL_BASE_URL") {
			t.Fatalf("a test key without a test host: %v\n%s", err, out)
		}
	})
}

// On a machine whose openssl cannot check Ed25519 (a Mac's LibreSSL),
// ssh-keygen checks the SSH copy of the signature instead; a release without
// that copy is refused, not installed unchecked.
func TestInstallScriptChecksTheSSHSignatureWithoutOpenSSL(t *testing.T) {
	keygen := sshKeygenWithSign(t)
	if keygen == "" {
		t.Skip("no ssh-keygen here (RELEASE_TEST_SSH_KEYGEN names one)")
	}
	r := newInstallRelease(t, "v1.4.0", time.Now())
	good, sig := r.get("manifest.json"), r.get(release.SSHSignatureFile)
	t.Run("good release", func(t *testing.T) {
		r.t = t
		r.installed(r.run("", keygen))
	})
	t.Run("tampered manifest", func(t *testing.T) {
		r.t = t
		r.set("manifest.json", bytes.Replace(good, []byte(`"channel": "stable"`), []byte(`"channel": "beta"`), 1))
		defer r.set("manifest.json", good)
		r.refused(r.run("", keygen), "manifest.sshsig is not a signature by a Clawdline release key")
	})
	t.Run("signature by another key", func(t *testing.T) {
		r.t = t
		_, other, _ := ed25519.GenerateKey(nil)
		r.set(release.SSHSignatureFile, release.SignSSH(good, other))
		defer r.set(release.SSHSignatureFile, sig)
		r.refused(r.run("", keygen), "manifest.sshsig is not a signature by a Clawdline release key")
	})
	t.Run("no SSH signature and no OpenSSL", func(t *testing.T) {
		r.t = t
		r.set(release.SSHSignatureFile, nil)
		defer r.set(release.SSHSignatureFile, sig)
		r.refused(r.run("", keygen), "nothing here can check this release's Ed25519 signature")
	})
}

// Without --version the newest release is taken only while its signed
// committed_at is recent, and a release older than the installed one only
// with --allow-downgrade; --version installs an old release on purpose.
func TestInstallScriptRefusesAStaleNewestOrADowngradeUnlessAsked(t *testing.T) {
	openssl := opensslWithEd25519(t)
	keygen := sshKeygenWithSign(t)
	if openssl == "" && keygen == "" {
		t.Skip("neither OpenSSL 3 nor ssh-keygen here")
	}
	old := time.Now().Add(-release.MaxManifestAge() - 48*time.Hour)
	r := newInstallRelease(t, "v1.0.0", old)
	r.refused(r.run(openssl, keygen), "more than the 180 days a newest release may be")
	r.installed(r.run(openssl, keygen, "--version", "v1.0.0"))
	r.refused(r.run(openssl, keygen, "--version", "v1.0.1"), "asked for v1.0.1 and the signed manifest")
	r.publish(old, func(m *release.Manifest) { m.CommittedAt = "" })
	r.refused(r.run(openssl, keygen), "carries no committed_at")

	for _, c := range []struct {
		installed, served string
		older             bool
	}{
		{"v1.2.0", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-beta.3", true},
		{"v1.0.0-beta.10", "v1.0.0-beta.9", true},
		{"v1.0.7", "v1.0.7-dev.4c41b18", true},
		{"v1.0.0-beta.9", "v1.0.0-beta.10", false},
		{"v1.0.7-dev.4c41b18", "v1.0.7", false},
		{"v0.9.9", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
	} {
		t.Run(c.installed+" then "+c.served, func(t *testing.T) {
			r := newInstallRelease(t, c.served, time.Now())
			if err := os.MkdirAll(filepath.Join(r.root, "releases", c.installed), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("releases/"+c.installed, filepath.Join(r.root, "current")); err != nil {
				t.Fatal(err)
			}
			res := r.run(openssl, keygen)
			if !c.older {
				r.installed(res)
				return
			}
			r.refused(res, "is older than the installed "+c.installed)
			r.installed(r.run(openssl, keygen, "--allow-downgrade"))
		})
	}
}

// install.sh carries the release keys keys.go trusts, the manifest age bound
// the updater holds the newest release to, and the SSH namespace the release
// tool signs in; a change to one without the other fails here.
func TestInstallScriptCarriesTheReleaseKeysAndBounds(t *testing.T) {
	b, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	value := func(name string) string {
		m := regexp.MustCompile(`(?m)^` + name + `='?([^'\n]*)'?$`).FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("install.sh sets no %s", name)
		}
		return m[1]
	}
	var want []string
	for _, k := range release.TrustedKeys() {
		want = append(want, base64.StdEncoding.EncodeToString(k))
	}
	got := strings.Fields(value("release_keys"))
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("install.sh trusts %v; internal/adapters/release/keys.go trusts %v", got, want)
	}
	days, err := strconv.Atoi(value("max_manifest_age_days"))
	if err != nil || time.Duration(days)*24*time.Hour != release.MaxManifestAge() {
		t.Errorf("install.sh's max_manifest_age_days is %q; release.MaxManifestAge is %v", value("max_manifest_age_days"), release.MaxManifestAge())
	}
	if ns := value("ssh_namespace"); ns != release.SSHNamespace {
		t.Errorf("install.sh verifies in namespace %q; releases are signed in %q", ns, release.SSHNamespace)
	}
}

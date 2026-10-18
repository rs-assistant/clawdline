package cloud

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

type fakeViewerKeys struct {
	dir   string
	key   domain.DeviceKey
	found bool
	err   error
}

func (f fakeViewerKeys) Dir() string { return f.dir }
func (f fakeViewerKeys) ViewerKey() (domain.DeviceKey, bool, error) {
	return f.key, f.found, f.err
}

// The local viewer line and the CLI ask one question; each typed answer is
// what they branch on.
func TestViewerAuthorizationSaysWhyThereIsNone(t *testing.T) {
	key, err := domain.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := ViewerIdentity{AccountID: "a", DeviceID: "d", ViewerCredential: "c", APIBase: "https://api.example"}
	saved := t.TempDir()
	if err := NewViewerIdentityStore(saved).Save(identity); err != nil {
		t.Fatal(err)
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ViewerIdentityFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	diskErr := errors.New("disk said no")
	for _, c := range []struct {
		name string
		keys fakeViewerKeys
		api  string
		want error
	}{
		{"absent", fakeViewerKeys{dir: t.TempDir(), key: key, found: true}, identity.APIBase, ErrViewerIdentityAbsent},
		{"unreadable identity", fakeViewerKeys{dir: broken, key: key, found: true}, identity.APIBase, ErrViewerIdentityUnreadable},
		{"another API", fakeViewerKeys{dir: saved, key: key, found: true}, "https://other.example", ErrViewerEnvironmentMismatch},
		// The identity is judged first: a key is no use to it.
		{"another API, no key", fakeViewerKeys{dir: saved}, "https://other.example", ErrViewerEnvironmentMismatch},
		{"no key", fakeViewerKeys{dir: saved}, identity.APIBase, ErrViewerKeyMissing},
		{"unreadable key", fakeViewerKeys{dir: saved, err: diskErr}, identity.APIBase, ErrViewerKeyUnreadable},
		{"ready", fakeViewerKeys{dir: saved, key: key, found: true}, identity.APIBase, nil},
	} {
		gotIdentity, gotKey, err := LoadViewerAuthorization(c.keys, c.api)
		if c.want == nil {
			if err != nil || gotIdentity != identity || gotKey.Fingerprint() != key.Fingerprint() {
				t.Errorf("%s: %+v %v", c.name, gotIdentity, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: answered %v, want %v", c.name, err, c.want)
		}
	}
	// An unreadable key still reads, and unwraps, as what the disk said.
	_, _, err = LoadViewerAuthorization(fakeViewerKeys{dir: saved, err: diskErr}, identity.APIBase)
	if err.Error() != diskErr.Error() || !errors.Is(err, diskErr) {
		t.Fatalf("unreadable key read as %q", err)
	}
}

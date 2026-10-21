package release

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
)

func TestVersionCannotBecomeAPathOrURL(t *testing.T) {
	for _, s := range []string{"v1.2.3-beta/../../outside", `v1.2.3-beta\outside`, "v1.2.3-beta?query", "v1.2.3-beta#fragment", "v1.2.3-beta%2foutside", "v1.2.3-beta\x00", "v+1.2.3"} {
		if _, err := ParseVersion(s); code(err) != CodeVersionUnparseable {
			t.Errorf("unsafe version %q accepted: %v", s, err)
		}
	}
	for _, s := range []string{"v1.2.3", "v1.2.3-beta.2", "v1.2.3-dev.abcdef0", "v1.2.3-rc-1"} {
		if v, err := ParseVersion(s); err != nil || v.String() != s {
			t.Errorf("valid version %q refused: %v", s, err)
		}
	}
}

func TestSignedManifestVersionCannotEscapeTheReleaseLayout(t *testing.T) {
	pub, priv := key(t)
	var m Manifest
	if err := json.Unmarshal(manifestBytes(t), &m); err != nil {
		t.Fatal(err)
	}
	// The artifact keeps its plain, valid name. Only the version used by
	// ReleaseDir and the explicit-version fetch URL attempts traversal.
	m.Version = "v1.2.3-beta/../../outside"
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(body, sigs(t, Sign(body, priv)), []ed25519.PublicKey{pub}); code(err) != CodeVersionUnparseable {
		t.Fatalf("signed manifest has unsafe release directory: %v", err)
	}
}

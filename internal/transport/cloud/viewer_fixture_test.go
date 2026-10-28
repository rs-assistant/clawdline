package cloud

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// testViewer is a browser paired with a link built by Open: its pin is in the
// link's own pin file, and it seals reads on that link's r/ channel.
type testViewer struct {
	id  string
	key domaincloud.DeviceKey
}

func pairTestViewer(t *testing.T, link *Link, id string) testViewer {
	t.Helper()
	key, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := link.pinned.Pin(link.identity.AccountID, adaptercloud.PinnedDevice{DeviceID: id,
		PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey()), Fingerprint: key.Fingerprint(),
		PairedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	return testViewer{id: id, key: key}
}

// seal writes one read on the machine's r/ channel, under secret or, when raw
// is given, under those bytes.
func (v testViewer) seal(t *testing.T, seq uint64, secret domaincloud.ContentKey, raw []byte) []byte {
	t.Helper()
	if raw != nil {
		var err error
		if secret, err = domaincloud.ContentKeyFromBytes(raw); err != nil {
			t.Fatal(err)
		}
	}
	env, err := domaincloud.Seal([]byte(`{"type":"places","session":"__clawdline_machine__","request":"r1"}`),
		domaincloud.SealParams{Ch: "r/mac_local", Seq: seq, Ts: uint64(time.Now().UnixMilli()),
			Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID, Sender: v.id, Key: secret, Signer: v.key})
	if err != nil {
		t.Fatal(err)
	}
	out, err := env.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (v testViewer) sealAt(t *testing.T, seq uint64, secret domaincloud.ContentKey, at time.Time) []byte {
	t.Helper()
	env, err := domaincloud.Seal([]byte(`{"type":"places","session":"__clawdline_machine__","request":"r1"}`),
		domaincloud.SealParams{Ch: "r/mac_local", Seq: seq, Ts: uint64(at.UnixMilli()),
			Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID, Sender: v.id, Key: secret, Signer: v.key})
	if err != nil {
		t.Fatal(err)
	}
	out, err := env.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

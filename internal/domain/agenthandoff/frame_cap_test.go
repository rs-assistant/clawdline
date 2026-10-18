package agenthandoff

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

// TestDiscoveryAndTaskStartRefuseAnOversizedCiphertext keeps the decoded
// ciphertext cap on the two frame kinds that have one. Since the four kinds
// share one open, the cap is a field of frameDomain, and dropping it would
// pass every other test: a correctly signed frame only one byte over still
// has to be refused before it is decrypted.
func TestDiscoveryAndTaskStartRefuseAnOversizedCiphertext(t *testing.T) {
	keys := newGoldenKeys(t)
	pair := goldenPair("pair-1", "mac-1", "linux-1")
	header := map[string]string{"request_id": "oversized-1"}
	for _, tc := range []struct {
		name   string
		domain frameDomain
	}{{"discovery", discoveryFrame}, {"task start", taskStartFrame}} {
		t.Run(tc.name, func(t *testing.T) {
			at := make([]byte, MaxBodyBytes)
			over := make([]byte, MaxBodyBytes+1)
			for _, c := range []struct {
				body []byte
				want error
			}{{at, nil}, {over, ErrPeerEnvelope}} {
				frame, err := tc.domain.seal(header, c.body, pair, keys.source, true, keys.sourceSigner)
				if err != nil {
					t.Fatal(err)
				}
				_, err = tc.domain.open(header, frame, pair, keys.target, false, keys.sourceSigner.Public().(ed25519.PublicKey))
				if !errors.Is(err, c.want) {
					t.Fatalf("a %d-byte body opened with %v, want %v", len(c.body), err, c.want)
				}
			}
		})
	}
}

package agenthandoff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

// frameDomain is what separates one machine frame kind from another: the
// signature and AAD labels, and how large a ciphertext Open will decode.
// The cryptography is identical for every kind.
type frameDomain struct {
	signatureLabel string
	aadLabel       string
	// maxCiphertext bounds the decoded ciphertext; 0 leaves it unbounded.
	maxCiphertext int
}

var (
	requestFrame   = frameDomain{signatureLabel: "clawdline-peer-v1", aadLabel: "clawdline-peer-aad-v1"}
	discoveryFrame = frameDomain{signatureLabel: "clawdline-peer-discovery-v1",
		aadLabel: "clawdline-peer-discovery-aad-v1", maxCiphertext: MaxBodyBytes + 16}
	taskStartFrame = frameDomain{signatureLabel: "clawdline-peer-task-start-v1",
		aadLabel: "clawdline-peer-task-start-aad-v1", maxCiphertext: MaxBodyBytes + 16}
	taskEventFrame = frameDomain{signatureLabel: "clawdline-peer-task-event-v1",
		aadLabel: "clawdline-peer-task-event-aad-v1"}
)

// sealedFrame holds the four fields every envelope kind carries beside its
// cleartext header.
type sealedFrame struct {
	Ciphertext, Nonce, KeyID, Signature string
}

func canonicalHeader(header any) ([]byte, error) {
	encoded, err := json.Marshal(header)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	parsed, err := cloud.Parse(encoded)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	return cloud.CanonicalBytes(parsed), nil
}

func (d frameDomain) signatureInput(header any, frame sealedFrame) ([]byte, error) {
	canonical, err := canonicalHeader(header)
	if err != nil || frame.Nonce == "" || frame.KeyID == "" || frame.Ciphertext == "" {
		return nil, ErrPeerEnvelope
	}
	return []byte(d.signatureLabel + "\n" + string(canonical) + "\n" +
		frame.Nonce + "\n" + frame.KeyID + "\n" + frame.Ciphertext), nil
}

func (d frameDomain) aad(canonical []byte, keyID string) []byte {
	return []byte(d.aadLabel + "\n" + string(canonical) + "\n" + keyID)
}

// seal encrypts body under the pair key with a fresh nonce and signs the
// canonical header with the ciphertext. sourceSide says which half of the
// pinned pair the local private key is. Callers validate the header first.
func (d frameDomain) seal(header any, body []byte, pair PairKeys, private *ecdh.PrivateKey,
	sourceSide bool, signer ed25519.PrivateKey) (sealedFrame, error) {
	key, keyID, err := PairKey(pair, private, sourceSide)
	if err != nil {
		return sealedFrame{}, ErrPeerEnvelope
	}
	aead, err := pairAEAD(key)
	if err != nil {
		return sealedFrame{}, ErrPeerEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return sealedFrame{}, ErrPeerEnvelope
	}
	canonical, err := canonicalHeader(header)
	if err != nil {
		return sealedFrame{}, ErrPeerEnvelope
	}
	frame := sealedFrame{KeyID: keyID, Nonce: base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, body, d.aad(canonical, keyID)))}
	input, err := d.signatureInput(header, frame)
	if err != nil {
		return sealedFrame{}, ErrPeerEnvelope
	}
	frame.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return frame, nil
}

// open verifies the pinned signature, the derived key ID and the GCM tag
// before returning plaintext. Callers check the plaintext against the header.
func (d frameDomain) open(header any, frame sealedFrame, pair PairKeys, private *ecdh.PrivateKey,
	sourceSide bool, pinnedSigner ed25519.PublicKey) ([]byte, error) {
	input, err := d.signatureInput(header, frame)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	sig, err := decodeCanonical(frame.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(pinnedSigner, input, sig) {
		return nil, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, private, sourceSide)
	if err != nil || frame.KeyID != keyID {
		return nil, ErrPeerEnvelope
	}
	nonce, err := decodeCanonical(frame.Nonce, 12)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	ciphertext, err := decodeCanonical(frame.Ciphertext, -1)
	if err != nil || len(ciphertext) < 16 || (d.maxCiphertext > 0 && len(ciphertext) > d.maxCiphertext) {
		return nil, ErrPeerEnvelope
	}
	aead, err := pairAEAD(key)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	canonical, err := canonicalHeader(header)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	body, err := aead.Open(nil, nonce, ciphertext, d.aad(canonical, keyID))
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

func pairAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

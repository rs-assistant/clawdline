package agenthandoff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

// DiscoveryEnvelope carries only a bounded inventory request or reply.
// Its separate cryptographic domain cannot authorize task or Session effects.
type DiscoveryEnvelope struct {
	Query      Discovery `json:"query"`
	Ciphertext string    `json:"ciphertext"`
	Nonce      string    `json:"nonce"`
	KeyID      string    `json:"key_id"`
	Signature  string    `json:"signature"`
}

func canonicalDiscovery(start Discovery) ([]byte, error) {
	encoded, err := json.Marshal(start)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	parsed, err := cloud.Parse(encoded)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	return cloud.CanonicalBytes(parsed), nil
}

func discoverySignatureInput(envelope DiscoveryEnvelope) ([]byte, error) {
	header, err := canonicalDiscovery(envelope.Query)
	if err != nil || envelope.Nonce == "" || envelope.KeyID == "" || envelope.Ciphertext == "" {
		return nil, ErrPeerEnvelope
	}
	return []byte("clawdline-peer-discovery-v1\n" + string(header) + "\n" +
		envelope.Nonce + "\n" + envelope.KeyID + "\n" + envelope.Ciphertext), nil
}

func discoveryAAD(header []byte, keyID string) []byte {
	return []byte("clawdline-peer-discovery-aad-v1\n" + string(header) + "\n" + keyID)
}

func SealDiscovery(start Discovery, body []byte, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (DiscoveryEnvelope, error) {
	if ValidateDiscovery(start, body) != nil || len(signer) != ed25519.PrivateKeySize ||
		start.PairID != pair.PairID || start.Source.MachineID != pair.SourceMachineID ||
		start.TargetMachineID != pair.TargetMachineID {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, private, !start.Reply)
	if err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	header, err := canonicalDiscovery(start)
	if err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	envelope := DiscoveryEnvelope{Query: start, KeyID: keyID,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, body, discoveryAAD(header, keyID)))}
	input, err := discoverySignatureInput(envelope)
	if err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return envelope, nil
}

func OpenDiscovery(envelope DiscoveryEnvelope, pair PairKeys, private *ecdh.PrivateKey,
	pinnedSigner ed25519.PublicKey) ([]byte, error) {
	if len(pinnedSigner) != ed25519.PublicKeySize || envelope.Query.PairID != pair.PairID ||
		envelope.Query.Source.MachineID != pair.SourceMachineID ||
		envelope.Query.TargetMachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	input, err := discoverySignatureInput(envelope)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	sig, err := decodeCanonical(envelope.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(pinnedSigner, input, sig) {
		return nil, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, private, envelope.Query.Reply)
	if err != nil || envelope.KeyID != keyID {
		return nil, ErrPeerEnvelope
	}
	nonce, err := decodeCanonical(envelope.Nonce, 12)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	ciphertext, err := decodeCanonical(envelope.Ciphertext, -1)
	if err != nil || len(ciphertext) < 16 || len(ciphertext) > MaxBodyBytes+16 {
		return nil, ErrPeerEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	header, err := canonicalDiscovery(envelope.Query)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	body, err := aead.Open(nil, nonce, ciphertext, discoveryAAD(header, keyID))
	if err != nil || ValidateDiscovery(envelope.Query, body) != nil {
		return nil, ErrPeerEnvelope
	}
	sum := sha256.Sum256(body)
	if envelope.Query.BodyDigest != hex.EncodeToString(sum[:]) {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

// Discovery binds a reply to a unique request and the requesting execution.
// Inventory content stays encrypted in both directions.
type Discovery struct {
	RequestID       string   `json:"request_id"`
	Source          Endpoint `json:"source"`
	TargetMachineID string   `json:"target_machine_id"`
	PairID          string   `json:"pair_id"`
	Reply           bool     `json:"reply"`
	BodyDigest      string   `json:"body_digest"`
}

func ValidateDiscovery(start Discovery, body []byte) error {
	sum := sha256.Sum256(body)
	if !validSegment(start.RequestID) || !validEndpoint(start.Source) ||
		!validSegment(start.TargetMachineID) || start.Source.MachineID == start.TargetMachineID ||
		!validSegment(start.PairID) || len(body) == 0 || len(body) > MaxBodyBytes ||
		start.BodyDigest != hex.EncodeToString(sum[:]) {
		return InvalidRequest
	}
	return nil
}

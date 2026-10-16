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

// TaskStartEnvelope uses a separate signature and encryption domain from
// Session-targeted peer envelopes. Its header names a machine and Project,
// never a target Session that does not exist yet.
type TaskStartEnvelope struct {
	Start      TaskStart `json:"start"`
	Ciphertext string    `json:"ciphertext"`
	Nonce      string    `json:"nonce"`
	KeyID      string    `json:"key_id"`
	Signature  string    `json:"signature"`
}

func canonicalTaskStart(start TaskStart) ([]byte, error) {
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

func taskStartSignatureInput(envelope TaskStartEnvelope) ([]byte, error) {
	header, err := canonicalTaskStart(envelope.Start)
	if err != nil || envelope.Nonce == "" || envelope.KeyID == "" || envelope.Ciphertext == "" {
		return nil, ErrPeerEnvelope
	}
	return []byte("clawdline-peer-task-start-v1\n" + string(header) + "\n" +
		envelope.Nonce + "\n" + envelope.KeyID + "\n" + envelope.Ciphertext), nil
}

func taskStartAAD(header []byte, keyID string) []byte {
	return []byte("clawdline-peer-task-start-aad-v1\n" + string(header) + "\n" + keyID)
}

func SealTaskStart(start TaskStart, body []byte, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (TaskStartEnvelope, error) {
	if ValidateTaskStart(start, body) != nil || len(signer) != ed25519.PrivateKeySize ||
		start.PairID != pair.PairID || start.Source.MachineID != pair.SourceMachineID ||
		start.TargetMachineID != pair.TargetMachineID {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, private, true)
	if err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	header, err := canonicalTaskStart(start)
	if err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	envelope := TaskStartEnvelope{Start: start, KeyID: keyID,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, body, taskStartAAD(header, keyID)))}
	input, err := taskStartSignatureInput(envelope)
	if err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return envelope, nil
}

func OpenTaskStart(envelope TaskStartEnvelope, pair PairKeys, private *ecdh.PrivateKey,
	pinnedSigner ed25519.PublicKey) ([]byte, error) {
	if len(pinnedSigner) != ed25519.PublicKeySize || envelope.Start.PairID != pair.PairID ||
		envelope.Start.Source.MachineID != pair.SourceMachineID ||
		envelope.Start.TargetMachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	input, err := taskStartSignatureInput(envelope)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	sig, err := decodeCanonical(envelope.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(pinnedSigner, input, sig) {
		return nil, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, private, false)
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
	header, err := canonicalTaskStart(envelope.Start)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	body, err := aead.Open(nil, nonce, ciphertext, taskStartAAD(header, keyID))
	if err != nil || ValidateTaskStart(envelope.Start, body) != nil {
		return nil, ErrPeerEnvelope
	}
	sum := sha256.Sum256(body)
	if envelope.Start.BodyDigest != hex.EncodeToString(sum[:]) {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

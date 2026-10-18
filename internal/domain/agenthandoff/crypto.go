package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

var ErrPeerEnvelope = errors.New("peer_envelope_invalid")

// PairKeys come only from a locally pinned, signed pairing transcript.
// An API response or a relay hint cannot directly construct trusted PairKeys.
type PairKeys struct {
	PairID              string
	SourceMachineID     string
	TargetMachineID     string
	SourceEncryptionKey string
	TargetEncryptionKey string
}

// Envelope is the private peer rail's peer_envelope body. SourcePublicKey is
// deliberately absent: it is an untrusted lookup hint, not verification data.
type Envelope struct {
	Request    Request `json:"request"`
	Ciphertext string  `json:"ciphertext"`
	Nonce      string  `json:"nonce"`
	KeyID      string  `json:"key_id"`
	Signature  string  `json:"signature"`
}

// PairKey derives the exact pair key and key ID from a pinned transcript.
// The caller retains its own X25519 private key in owner-only storage.
func PairKey(pair PairKeys, localPrivate *ecdh.PrivateKey, sourceSide bool) ([]byte, string, error) {
	if pair.PairID == "" || pair.SourceMachineID == "" || pair.TargetMachineID == "" || localPrivate == nil {
		return nil, "", ErrPeerEnvelope
	}
	sourcePublic, err := decodeCanonical(pair.SourceEncryptionKey, 32)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	targetPublic, err := decodeCanonical(pair.TargetEncryptionKey, 32)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	local, remote := targetPublic, sourcePublic
	if sourceSide {
		local, remote = sourcePublic, targetPublic
	}
	if !bytes.Equal(localPrivate.PublicKey().Bytes(), local) {
		return nil, "", ErrPeerEnvelope
	}
	peerPublic, err := ecdh.X25519().NewPublicKey(remote)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	shared, err := localPrivate.ECDH(peerPublic)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	salt := sha256.Sum256([]byte("clawdline-peer-kdf-v1\n" + pair.PairID + "\n" +
		pair.SourceMachineID + "\n" + pair.TargetMachineID))
	info := []byte("clawdline-peer-aes-gcm-v1\n" + pair.SourceEncryptionKey + "\n" + pair.TargetEncryptionKey)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt[:], info), key); err != nil {
		return nil, "", ErrPeerEnvelope
	}
	idInput := "clawdline-peer-key-id-v1\n" + pair.PairID + "\n" +
		pair.SourceEncryptionKey + "\n" + pair.TargetEncryptionKey
	idSum := sha256.Sum256([]byte(idInput))
	return key, "peer-" + hex.EncodeToString(idSum[:])[:32], nil
}

// SignatureInput is the peer rail's Ed25519 input, separate from the older
// Cloud envelope signature format.
func SignatureInput(envelope Envelope) ([]byte, error) {
	return requestFrame.signatureInput(envelope.Request, envelope.frame())
}

func (envelope Envelope) frame() sealedFrame {
	return sealedFrame{Ciphertext: envelope.Ciphertext, Nonce: envelope.Nonce,
		KeyID: envelope.KeyID, Signature: envelope.Signature}
}

// Open verifies the pinned peer signature, derived key ID and GCM tag before
// returning plaintext. Admission and receipt claims follow this operation.
func Open(envelope Envelope, pair PairKeys, localPrivate *ecdh.PrivateKey, pinnedSigner ed25519.PublicKey) ([]byte, error) {
	if len(pinnedSigner) != ed25519.PublicKeySize ||
		envelope.Request.Source.MachineID != pair.SourceMachineID ||
		envelope.Request.Target.MachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	plain, err := requestFrame.open(envelope.Request, envelope.frame(), pair, localPrivate, false, pinnedSigner)
	if err != nil || len(plain) == 0 {
		return nil, ErrPeerEnvelope
	}
	sum := sha256.Sum256(plain)
	if envelope.Request.BodyDigest != hex.EncodeToString(sum[:]) {
		return nil, ErrPeerEnvelope
	}
	return plain, nil
}

// Seal encrypts exactly one request with a fresh nonce and signs its routing
// header and ciphertext as the source machine. The caller must first verify
// its local grant and source execution; Cloud performs a separate fresh check.
func Seal(request Request, body []byte, pair PairKeys, localPrivate *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (Envelope, error) {
	if len(body) == 0 || len(signer) != ed25519.PrivateKeySize ||
		request.Source.MachineID != pair.SourceMachineID ||
		request.Target.MachineID != pair.TargetMachineID {
		return Envelope{}, ErrPeerEnvelope
	}
	sum := sha256.Sum256(body)
	if request.BodyDigest != hex.EncodeToString(sum[:]) {
		return Envelope{}, ErrPeerEnvelope
	}
	frame, err := requestFrame.seal(request, body, pair, localPrivate, true, signer)
	if err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	return Envelope{Request: request, Ciphertext: frame.Ciphertext, Nonce: frame.Nonce,
		KeyID: frame.KeyID, Signature: frame.Signature}, nil
}

func decodeCanonical(raw string, length int) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || (length >= 0 && len(decoded) != length) ||
		base64.StdEncoding.EncodeToString(decoded) != raw {
		return nil, ErrPeerEnvelope
	}
	return decoded, nil
}

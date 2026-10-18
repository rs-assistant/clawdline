package agenthandoff

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
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

func SealDiscovery(start Discovery, body []byte, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (DiscoveryEnvelope, error) {
	if ValidateDiscovery(start, body) != nil || len(signer) != ed25519.PrivateKeySize ||
		start.PairID != pair.PairID || start.Source.MachineID != pair.SourceMachineID ||
		start.TargetMachineID != pair.TargetMachineID {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	frame, err := discoveryFrame.seal(start, body, pair, private, !start.Reply, signer)
	if err != nil {
		return DiscoveryEnvelope{}, ErrPeerEnvelope
	}
	return DiscoveryEnvelope{Query: start, Ciphertext: frame.Ciphertext, Nonce: frame.Nonce,
		KeyID: frame.KeyID, Signature: frame.Signature}, nil
}

func OpenDiscovery(envelope DiscoveryEnvelope, pair PairKeys, private *ecdh.PrivateKey,
	pinnedSigner ed25519.PublicKey) ([]byte, error) {
	if len(pinnedSigner) != ed25519.PublicKeySize || envelope.Query.PairID != pair.PairID ||
		envelope.Query.Source.MachineID != pair.SourceMachineID ||
		envelope.Query.TargetMachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	body, err := discoveryFrame.open(envelope.Query, envelope.frame(), pair, private, envelope.Query.Reply, pinnedSigner)
	if err != nil || ValidateDiscovery(envelope.Query, body) != nil {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

func (envelope DiscoveryEnvelope) frame() sealedFrame {
	return sealedFrame{Ciphertext: envelope.Ciphertext, Nonce: envelope.Nonce,
		KeyID: envelope.KeyID, Signature: envelope.Signature}
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

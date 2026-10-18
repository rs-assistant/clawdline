package agenthandoff

import (
	"crypto/ecdh"
	"crypto/ed25519"
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

func SealTaskStart(start TaskStart, body []byte, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (TaskStartEnvelope, error) {
	if ValidateTaskStart(start, body) != nil || len(signer) != ed25519.PrivateKeySize ||
		start.PairID != pair.PairID || start.Source.MachineID != pair.SourceMachineID ||
		start.TargetMachineID != pair.TargetMachineID {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	frame, err := taskStartFrame.seal(start, body, pair, private, true, signer)
	if err != nil {
		return TaskStartEnvelope{}, ErrPeerEnvelope
	}
	return TaskStartEnvelope{Start: start, Ciphertext: frame.Ciphertext, Nonce: frame.Nonce,
		KeyID: frame.KeyID, Signature: frame.Signature}, nil
}

func OpenTaskStart(envelope TaskStartEnvelope, pair PairKeys, private *ecdh.PrivateKey,
	pinnedSigner ed25519.PublicKey) ([]byte, error) {
	if len(pinnedSigner) != ed25519.PublicKeySize || envelope.Start.PairID != pair.PairID ||
		envelope.Start.Source.MachineID != pair.SourceMachineID ||
		envelope.Start.TargetMachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	body, err := taskStartFrame.open(envelope.Start, envelope.frame(), pair, private, false, pinnedSigner)
	if err != nil || ValidateTaskStart(envelope.Start, body) != nil {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

func (envelope TaskStartEnvelope) frame() sealedFrame {
	return sealedFrame{Ciphertext: envelope.Ciphertext, Nonce: envelope.Nonce,
		KeyID: envelope.KeyID, Signature: envelope.Signature}
}

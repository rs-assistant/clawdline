package agenthandoff

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

const (
	TaskEventOpened  = "opened"
	TaskEventMessage = "message"
	TaskEventFinish  = "finish"
)

// TaskEvent is bound to the original start and both exact Session executions.
// The first opened event establishes the target execution at the source.
type TaskEvent struct {
	Start      TaskStart `json:"start"`
	RequestID  string    `json:"request_id"`
	Kind       string    `json:"kind"`
	Sender     Endpoint  `json:"sender"`
	Recipient  Endpoint  `json:"recipient"`
	BodyDigest string    `json:"body_digest"`
}

type TaskEventEnvelope struct {
	Event      TaskEvent `json:"event"`
	Ciphertext string    `json:"ciphertext"`
	Nonce      string    `json:"nonce"`
	KeyID      string    `json:"key_id"`
	Signature  string    `json:"signature"`
}

func ValidateTaskEvent(event TaskEvent, body []byte) error {
	if ValidateTaskEventHeader(event) != nil || len(body) == 0 || len(body) > MaxBodyBytes {
		return InvalidRequest
	}
	sum := sha256.Sum256(body)
	if event.BodyDigest != hex.EncodeToString(sum[:]) {
		return InvalidRequest
	}
	return nil
}

func ValidateTaskEventHeader(event TaskEvent) error {
	if ValidateTaskStartHeader(event.Start) != nil || !validSegment(event.RequestID) ||
		(event.Kind != TaskEventOpened && event.Kind != TaskEventMessage && event.Kind != TaskEventFinish) ||
		!validEndpoint(event.Sender) || !validEndpoint(event.Recipient) ||
		!digest.MatchString(event.BodyDigest) {
		return InvalidRequest
	}
	source, target := event.Start.Source.MachineID, event.Start.TargetMachineID
	if !((event.Sender.MachineID == source && event.Recipient.MachineID == target &&
		event.Sender == event.Start.Source) ||
		(event.Sender.MachineID == target && event.Recipient == event.Start.Source)) {
		return InvalidRequest
	}
	if (event.Kind == TaskEventOpened || event.Kind == TaskEventFinish) && event.Sender.MachineID != target {
		return InvalidRequest
	}
	return nil
}

// SealTaskEvent accepts either direction on the same directed pair. The
// sender's local private key decides which half of the pinned pair is local.
func SealTaskEvent(event TaskEvent, body []byte, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (TaskEventEnvelope, error) {
	if ValidateTaskEvent(event, body) != nil || len(signer) != ed25519.PrivateKeySize ||
		event.Start.PairID != pair.PairID || event.Start.Source.MachineID != pair.SourceMachineID ||
		event.Start.TargetMachineID != pair.TargetMachineID {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	frame, err := taskEventFrame.seal(event, body, pair, private, event.Sender.MachineID == pair.SourceMachineID, signer)
	if err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	return TaskEventEnvelope{Event: event, Ciphertext: frame.Ciphertext, Nonce: frame.Nonce,
		KeyID: frame.KeyID, Signature: frame.Signature}, nil
}

func OpenTaskEvent(envelope TaskEventEnvelope, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PublicKey) ([]byte, error) {
	event := envelope.Event
	if len(signer) != ed25519.PublicKeySize || event.Start.PairID != pair.PairID ||
		event.Start.Source.MachineID != pair.SourceMachineID || event.Start.TargetMachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	body, err := taskEventFrame.open(envelope.Event, envelope.frame(), pair, private, event.Recipient.MachineID == pair.SourceMachineID, signer)
	if err != nil || ValidateTaskEvent(event, body) != nil {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

func (envelope TaskEventEnvelope) frame() sealedFrame {
	return sealedFrame{Ciphertext: envelope.Ciphertext, Nonce: envelope.Nonce,
		KeyID: envelope.KeyID, Signature: envelope.Signature}
}

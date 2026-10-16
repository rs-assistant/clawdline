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

func canonicalTaskEvent(event TaskEvent) ([]byte, error) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	parsed, err := cloud.Parse(encoded)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	return cloud.CanonicalBytes(parsed), nil
}

func taskEventSignatureInput(envelope TaskEventEnvelope) ([]byte, error) {
	header, err := canonicalTaskEvent(envelope.Event)
	if err != nil || envelope.Nonce == "" || envelope.KeyID == "" || envelope.Ciphertext == "" {
		return nil, ErrPeerEnvelope
	}
	return []byte("clawdline-peer-task-event-v1\n" + string(header) + "\n" +
		envelope.Nonce + "\n" + envelope.KeyID + "\n" + envelope.Ciphertext), nil
}

func taskEventAAD(header []byte, keyID string) []byte {
	return []byte("clawdline-peer-task-event-aad-v1\n" + string(header) + "\n" + keyID)
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
	key, keyID, err := PairKey(pair, private, event.Sender.MachineID == pair.SourceMachineID)
	if err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	header, err := canonicalTaskEvent(event)
	if err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	envelope := TaskEventEnvelope{Event: event, KeyID: keyID,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, body, taskEventAAD(header, keyID)))}
	input, err := taskEventSignatureInput(envelope)
	if err != nil {
		return TaskEventEnvelope{}, ErrPeerEnvelope
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return envelope, nil
}

func OpenTaskEvent(envelope TaskEventEnvelope, pair PairKeys, private *ecdh.PrivateKey,
	signer ed25519.PublicKey) ([]byte, error) {
	event := envelope.Event
	if len(signer) != ed25519.PublicKeySize || event.Start.PairID != pair.PairID ||
		event.Start.Source.MachineID != pair.SourceMachineID || event.Start.TargetMachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	input, err := taskEventSignatureInput(envelope)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	sig, err := decodeCanonical(envelope.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(signer, input, sig) {
		return nil, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, private, event.Recipient.MachineID == pair.SourceMachineID)
	if err != nil || envelope.KeyID != keyID {
		return nil, ErrPeerEnvelope
	}
	nonce, err := decodeCanonical(envelope.Nonce, 12)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	ciphertext, err := decodeCanonical(envelope.Ciphertext, -1)
	if err != nil || len(ciphertext) < 16 {
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
	header, err := canonicalTaskEvent(event)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	body, err := aead.Open(nil, nonce, ciphertext, taskEventAAD(header, keyID))
	if err != nil || ValidateTaskEvent(event, body) != nil {
		return nil, ErrPeerEnvelope
	}
	return body, nil
}

package agenthandoff

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

const (
	TaskProbeChallenge = "challenge"
	TaskProbeProof     = "proof"
)

// TaskProbe lets the target challenge the source Session immediately before
// opening an Agent. Neither the relay nor a stale signed start can assert
// that a source execution is still current.
type TaskProbe struct {
	Start     TaskStart `json:"start"`
	Kind      string    `json:"kind"`
	Nonce     string    `json:"nonce"`
	Signature string    `json:"signature"`
}

func taskProbeInput(probe TaskProbe) ([]byte, error) {
	if ValidateTaskStartHeader(probe.Start) != nil ||
		(probe.Kind != TaskProbeChallenge && probe.Kind != TaskProbeProof) {
		return nil, ErrPeerEnvelope
	}
	nonce, err := decodeCanonical(probe.Nonce, 24)
	if err != nil || len(nonce) != 24 {
		return nil, ErrPeerEnvelope
	}
	header, err := canonicalHeader(probe.Start)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	return []byte("clawdline-peer-task-probe-v1\n" + string(header) + "\n" +
		probe.Kind + "\n" + probe.Nonce), nil
}

func NewTaskChallenge(start TaskStart, signer ed25519.PrivateKey) (TaskProbe, error) {
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return TaskProbe{}, err
	}
	return SignTaskProbe(TaskProbe{Start: start, Kind: TaskProbeChallenge,
		Nonce: base64.StdEncoding.EncodeToString(nonce)}, signer)
}

// SignTaskProbe signs an exact challenge or its reply. The caller proves its
// local Session generation separately before producing a proof.
func SignTaskProbe(probe TaskProbe, signer ed25519.PrivateKey) (TaskProbe, error) {
	if len(signer) != ed25519.PrivateKeySize || probe.Signature != "" {
		return TaskProbe{}, ErrPeerEnvelope
	}
	input, err := taskProbeInput(probe)
	if err != nil {
		return TaskProbe{}, err
	}
	probe.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return probe, nil
}

func VerifyTaskProbe(probe TaskProbe, signer ed25519.PublicKey) error {
	if len(signer) != ed25519.PublicKeySize {
		return ErrPeerEnvelope
	}
	input, err := taskProbeInput(probe)
	if err != nil {
		return err
	}
	signature, err := decodeCanonical(probe.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(signer, input, signature) {
		return ErrPeerEnvelope
	}
	return nil
}

// AnswerTaskChallenge copies the one-time nonce and bound start only after
// the target's pinned signature has been verified.
func AnswerTaskChallenge(challenge TaskProbe, target ed25519.PublicKey,
	source ed25519.PrivateKey) (TaskProbe, error) {
	if challenge.Kind != TaskProbeChallenge || VerifyTaskProbe(challenge, target) != nil {
		return TaskProbe{}, ErrPeerEnvelope
	}
	return SignTaskProbe(TaskProbe{Start: challenge.Start, Kind: TaskProbeProof,
		Nonce: challenge.Nonce}, source)
}

// MatchTaskProof rejects a reply to another task, execution or nonce even if
// it was signed by the paired machine.
func MatchTaskProof(challenge, proof TaskProbe, source ed25519.PublicKey) error {
	if challenge.Kind != TaskProbeChallenge || proof.Kind != TaskProbeProof ||
		challenge.Start != proof.Start || challenge.Nonce != proof.Nonce {
		return errors.New("peer_task_probe_mismatch")
	}
	return VerifyTaskProbe(proof, source)
}

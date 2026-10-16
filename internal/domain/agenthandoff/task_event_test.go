package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestTaskEventSealsBothDirectionsAndPinsBothExecutions(t *testing.T) {
	source, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	target, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	start, _ := validTaskStart()
	pair := PairKeys{PairID: start.PairID, SourceMachineID: start.Source.MachineID,
		TargetMachineID:     start.TargetMachineID,
		SourceEncryptionKey: "e06Qm75//kTEZaIgA31gjuNYl9Me+XLwf3SJLLD3PxM=",
		TargetEncryptionKey: "D6poTtKIZ7l/Smot7l34zpdOdrcBjj8iocTPJnhXDyA="}
	sourceSigner := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32))
	targetSigner := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32))
	targetEndpoint := Endpoint{MachineID: start.TargetMachineID, SessionID: "target-session",
		ExecutionGeneration: "fedcba9876543210fedcba9876543210"}
	body := []byte(`{"state":"opened"}`)
	sum := sha256.Sum256(body)
	event := TaskEvent{Start: start, RequestID: "event-1", Kind: TaskEventOpened,
		Sender: targetEndpoint, Recipient: start.Source, BodyDigest: hex.EncodeToString(sum[:])}
	envelope, err := SealTaskEvent(event, body, pair, target, targetSigner)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenTaskEvent(envelope, pair, source, targetSigner.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(opened, body) {
		t.Fatalf("target to source: %q, %v", opened, err)
	}
	changed := envelope
	changed.Event.Sender.ExecutionGeneration = "00000000000000000000000000000000"
	if _, err := OpenTaskEvent(changed, pair, source, targetSigner.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("a replaced target reused its event")
	}
	if _, err := OpenTaskEvent(envelope, pair, source, sourceSigner.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("the source key impersonated the target")
	}
	body = []byte("Please check one more directory.")
	sum = sha256.Sum256(body)
	event = TaskEvent{Start: start, RequestID: "event-2", Kind: TaskEventMessage,
		Sender: start.Source, Recipient: targetEndpoint, BodyDigest: hex.EncodeToString(sum[:])}
	envelope, err = SealTaskEvent(event, body, pair, source, sourceSigner)
	if err != nil {
		t.Fatal(err)
	}
	opened, err = OpenTaskEvent(envelope, pair, target, sourceSigner.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(opened, body) {
		t.Fatalf("source to target: %q, %v", opened, err)
	}
}

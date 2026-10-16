package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"testing"
)

func TestTaskStartEnvelopeBindsMachineProjectAndBody(t *testing.T) {
	source, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	target, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	start, body := validTaskStart()
	pair := PairKeys{PairID: start.PairID, SourceMachineID: start.Source.MachineID,
		TargetMachineID:     start.TargetMachineID,
		SourceEncryptionKey: "e06Qm75//kTEZaIgA31gjuNYl9Me+XLwf3SJLLD3PxM=",
		TargetEncryptionKey: "D6poTtKIZ7l/Smot7l34zpdOdrcBjj8iocTPJnhXDyA="}
	signer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32))
	envelope, err := SealTaskStart(start, body, pair, source, signer)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenTaskStart(envelope, pair, target, signer.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(opened, body) {
		t.Fatalf("opened %q: %v", opened, err)
	}
	changed := envelope
	changed.Start.TargetProjectID = "another-project"
	if _, err := OpenTaskStart(changed, pair, target, signer.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("changed target Project kept the original signature")
	}
	changed = envelope
	changed.Start.Source.ExecutionGeneration = "fedcba9876543210fedcba9876543210"
	if _, err := OpenTaskStart(changed, pair, target, signer.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("changed source execution kept the original signature")
	}
	if _, err := OpenTaskStart(envelope, pair, target,
		ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32)).Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("unpaired signing key opened the task")
	}
	if _, err := SealTaskStart(start, []byte("different body"), pair, source, signer); err == nil {
		t.Fatal("body outside digest was sealed")
	}
}

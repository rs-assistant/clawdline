package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestDiscoveryEncryptsBothDirectionsAndPinsRequestIdentity(t *testing.T) {
	source, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	target, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	start, _ := validTaskStart()
	pair := PairKeys{PairID: start.PairID, SourceMachineID: start.Source.MachineID,
		TargetMachineID: start.TargetMachineID, SourceEncryptionKey: "e06Qm75//kTEZaIgA31gjuNYl9Me+XLwf3SJLLD3PxM=",
		TargetEncryptionKey: "D6poTtKIZ7l/Smot7l34zpdOdrcBjj8iocTPJnhXDyA="}
	for _, reply := range []bool{false, true} {
		body := []byte(`{"projects":[{"id":"p1"}],"sessions":[]}`)
		sum := sha256.Sum256(body)
		q := Discovery{RequestID: "query-1", Source: start.Source, TargetMachineID: start.TargetMachineID,
			PairID: start.PairID, Reply: reply, BodyDigest: hex.EncodeToString(sum[:])}
		signer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32))
		from, to := source, target
		if reply {
			from, to = target, source
		}
		envelope, err := SealDiscovery(q, body, pair, from, signer)
		if err != nil {
			t.Fatal(err)
		}
		got, err := OpenDiscovery(envelope, pair, to, signer.Public().(ed25519.PublicKey))
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("reply %v: %s %v", reply, got, err)
		}
		for _, mutate := range []func(*DiscoveryEnvelope){
			func(e *DiscoveryEnvelope) { e.Query.RequestID = "other" },
			func(e *DiscoveryEnvelope) { e.Query.Reply = !e.Query.Reply },
			func(e *DiscoveryEnvelope) {
				e.Query.Source.ExecutionGeneration = "f" + e.Query.Source.ExecutionGeneration[1:]
			},
			func(e *DiscoveryEnvelope) { e.Ciphertext = "AAAA" },
		} {
			changed := envelope
			mutate(&changed)
			if _, err := OpenDiscovery(changed, pair, to, signer.Public().(ed25519.PublicKey)); err == nil {
				t.Fatal("changed envelope opened")
			}
		}
		wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32))
		if _, err := OpenDiscovery(envelope, pair, to, wrong.Public().(ed25519.PublicKey)); err == nil {
			t.Fatal("wrong signer opened inventory")
		}
	}
}

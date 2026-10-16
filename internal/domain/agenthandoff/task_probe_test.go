package agenthandoff

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestTaskProbeBindsTheCurrentSourceChallenge(t *testing.T) {
	_, source, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, target, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	start := TaskStart{TaskID: "11111111-1111-4111-8111-111111111111", RequestID: "request-1",
		Source: Endpoint{MachineID: "source", SessionID: "session",
			ExecutionGeneration: "11111111111111111111111111111111"},
		TargetMachineID: "target", TargetProjectID: "project", PairID: "pair",
		BodyDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	challenge, err := NewTaskChallenge(start, target)
	if err != nil || VerifyTaskProbe(challenge, target.Public().(ed25519.PublicKey)) != nil {
		t.Fatalf("target challenge: %v", err)
	}
	proof, err := AnswerTaskChallenge(challenge, target.Public().(ed25519.PublicKey), source)
	if err != nil || MatchTaskProof(challenge, proof, source.Public().(ed25519.PublicKey)) != nil {
		t.Fatalf("source proof: %v", err)
	}
	changed := proof
	changed.Start.Source.ExecutionGeneration = "22222222222222222222222222222222"
	if MatchTaskProof(challenge, changed, source.Public().(ed25519.PublicKey)) == nil {
		t.Fatal("a replaced source execution reused its proof")
	}
	changed = proof
	changed.Nonce = challenge.Nonce[:len(challenge.Nonce)-4] + "AAAA"
	if MatchTaskProof(challenge, changed, source.Public().(ed25519.PublicKey)) == nil {
		t.Fatal("a different challenge reused its proof")
	}
	changed = challenge
	changed.Start.TargetProjectID = "another"
	if _, err := AnswerTaskChallenge(changed, target.Public().(ed25519.PublicKey), source); err == nil {
		t.Fatal("a changed target Project reused the target signature")
	}
}

package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// goldenNonce replaces the process random source for one seal so the
// envelope bytes are reproducible; Ed25519 and AES-GCM are otherwise
// deterministic for fixed keys and a fixed nonce.
func goldenNonce(t *testing.T) {
	t.Helper()
	previous := rand.Reader
	rand.Reader = bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	t.Cleanup(func() { rand.Reader = previous })
}

type goldenKeys struct {
	source, target             *ecdh.PrivateKey
	sourceSigner, targetSigner ed25519.PrivateKey
}

func newGoldenKeys(t *testing.T) goldenKeys {
	t.Helper()
	source, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	target, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return goldenKeys{source: source, target: target,
		sourceSigner: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32)),
		targetSigner: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32))}
}

func goldenPair(pairID, source, target string) PairKeys {
	return PairKeys{PairID: pairID, SourceMachineID: source, TargetMachineID: target,
		SourceEncryptionKey: "e06Qm75//kTEZaIgA31gjuNYl9Me+XLwf3SJLLD3PxM=",
		TargetEncryptionKey: "D6poTtKIZ7l/Smot7l34zpdOdrcBjj8iocTPJnhXDyA="}
}

func pinEnvelope(t *testing.T, envelope any, want string) {
	t.Helper()
	got, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("envelope bytes changed\n got: %s\nwant: %s", got, want)
	}
}

const (
	goldenRequestEnvelope             = `{"request":{"request_id":"request-1","kind":"message","source":{"machine_id":"machine-a","session_id":"session-a","execution_generation":"11111111111111111111111111111111"},"target":{"machine_id":"machine-b","session_id":"session-b","execution_generation":"22222222222222222222222222222222"},"grant_id":"grant-1","body_digest":"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},"ciphertext":"fLNO5h6c86ygSnst6rL5eGihEoJT","nonce":"AAECAwQFBgcICQoL","key_id":"peer-799207c9a5943290af8fddcf8e6d7e32","signature":"oWmcnrsOe84tzdbCa0kF5nxHmO9J3Rux1bF1RAGtQDlq0NUqlymEhlP6LVqiCXJT1fnkyyL1KiEkcRJWDThbAQ=="}`
	goldenDiscoveryQueryEnvelope      = `{"query":{"request_id":"query-1","source":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"target_machine_id":"linux-1","pair_id":"pair-1","reply":false,"body_digest":"7774189f2a12841b0b4744709a89a0396a02807cc1f2c89bc8bb6b6e67feb043"},"ciphertext":"4h3aR0+KyFSZhd6i8ECF/u/CazjasrW8RQNSVoeFf48tDgqEuv3fDJJ80QSD920QEYQ8MslT0D8=","nonce":"AAECAwQFBgcICQoL","key_id":"peer-2eef4da462bbf8735646d7dae12a8f92","signature":"7BtE5J0iqg0zxjFDF48qpM9ceTtSTGh8Risz+XxNgM+6g9IFIiHLfTxmArN4uCs8JLJjpNzVnLMjyUEScTFEDw=="}`
	goldenDiscoveryReplyEnvelope      = `{"query":{"request_id":"query-1","source":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"target_machine_id":"linux-1","pair_id":"pair-1","reply":true,"body_digest":"7774189f2a12841b0b4744709a89a0396a02807cc1f2c89bc8bb6b6e67feb043"},"ciphertext":"4h3aR0+KyFSZhd6i8ECF/u/CazjasrW8RQNSVoeFf48tDgqEuv3fDOq+wLfjxhedRQbLQgq+1qQ=","nonce":"AAECAwQFBgcICQoL","key_id":"peer-2eef4da462bbf8735646d7dae12a8f92","signature":"QhK2XfalsI0pc2DFIx3baPhkNx0KxIDlmi1ebaezg43o4lCGWzV6ArrKt3BGwM8FXJNYP440MPp3/2xTZP+VCw=="}`
	goldenTaskStartEnvelope           = `{"start":{"task_id":"11111111-1111-4111-8111-111111111111","request_id":"request-1","source":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"target_machine_id":"linux-1","target_project_id":"clawdline","pair_id":"pair-1","body_digest":"0d7865ffa5d220730b9acf7ed00694f9c6a57299910354b8e23b72e6b5e186a8"},"ciphertext":"4h3LRlOJ3kOMmIi6kRnE++qVNX+Ir7W1cVscQMDMLqIrExKG6cj0FOTmoV2mCTczNG9wERRdNQUyTwICX0sxjwLz4juGS1tMwtBVWrPRME+K1K/EfrSpaGDbWyCAJ7IeX/gikV6/rEPrfcXIS5edqE0cFe9fUAgCQoJA","nonce":"AAECAwQFBgcICQoL","key_id":"peer-2eef4da462bbf8735646d7dae12a8f92","signature":"qUyBff2tWDPP8nYMFHSDsNvpPnL9EshqNvmOdTkcnmdxOqchvZPSBGg0gpD/txi8rokVaXgISLVLnqelNmuhCg=="}`
	goldenTaskEventFromTargetEnvelope = `{"event":{"start":{"task_id":"11111111-1111-4111-8111-111111111111","request_id":"request-1","source":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"target_machine_id":"linux-1","target_project_id":"clawdline","pair_id":"pair-1","body_digest":"0d7865ffa5d220730b9acf7ed00694f9c6a57299910354b8e23b72e6b5e186a8"},"request_id":"event-1","kind":"opened","sender":{"machine_id":"linux-1","session_id":"target-session","execution_generation":"fedcba9876543210fedcba9876543210"},"recipient":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"body_digest":"ba3b8e462fa484463985fcaac64346b90a8fc510ca8eb96d308ac5edffa882ca"},"ciphertext":"4h3ZQUGUyBXX1JPozlXC86mdTVfurXSJzfMs4TWUJw0Uog==","nonce":"AAECAwQFBgcICQoL","key_id":"peer-2eef4da462bbf8735646d7dae12a8f92","signature":"OFF4Tx7VJs5eLKAkHhobKT1Tc+0WB37A6nXEBaTA8tSWlKaoVVoQaH8Fz4IjD6gXqq+fR0AV5fPtevovau74Aw=="}`
	goldenTaskEventFromSourceEnvelope = `{"event":{"start":{"task_id":"11111111-1111-4111-8111-111111111111","request_id":"request-1","source":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"target_machine_id":"linux-1","target_project_id":"clawdline","pair_id":"pair-1","body_digest":"0d7865ffa5d220730b9acf7ed00694f9c6a57299910354b8e23b72e6b5e186a8"},"request_id":"event-1","kind":"message","sender":{"machine_id":"mac-1","session_id":"session-1","execution_generation":"0123456789abcdef0123456789abcdef"},"recipient":{"machine_id":"linux-1","session_id":"target-session","execution_generation":"fedcba9876543210fedcba9876543210"},"body_digest":"459cac8f707a2944b5ce91efb1d06aa609f8e102b92b9210e6c83cf12b92d886"},"ciphertext":"yVPPVFOFjVSFk5/zi1TJ8quNPmjPo/OoakoTUY2EdchR8jJh7wEWSIkWG02yftnW","nonce":"AAECAwQFBgcICQoL","key_id":"peer-2eef4da462bbf8735646d7dae12a8f92","signature":"MhJ7AWdsUxhwiH1/mzpthlgZ5TGHKjbadjKRvOKApHkU/bkb0d7PXxB5AQlLxdihuVfN09ocHZp1UzTnFAPEAQ=="}`
)

func TestRequestEnvelopeBytesArePinned(t *testing.T) {
	keys := newGoldenKeys(t)
	request, body, _ := validRequestAndFacts()
	pair := goldenPair("11111111-1111-4111-8111-111111111111", "machine-a", "machine-b")
	goldenNonce(t)
	envelope, err := Seal(request, body, pair, keys.source, keys.sourceSigner)
	if err != nil {
		t.Fatal(err)
	}
	pinEnvelope(t, envelope, goldenRequestEnvelope)
	var pinned Envelope
	if err := json.Unmarshal([]byte(goldenRequestEnvelope), &pinned); err != nil {
		t.Fatal(err)
	}
	plain, err := Open(pinned, pair, keys.target, keys.sourceSigner.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(plain, body) {
		t.Fatalf("pinned request envelope opened as %q: %v", plain, err)
	}
}

func TestDiscoveryEnvelopeBytesArePinned(t *testing.T) {
	keys := newGoldenKeys(t)
	start, _ := validTaskStart()
	pair := goldenPair(start.PairID, start.Source.MachineID, start.TargetMachineID)
	body := []byte(`{"projects":[{"id":"p1"}],"sessions":[]}`)
	sum := sha256.Sum256(body)
	for _, tc := range []struct {
		reply    bool
		from, to *ecdh.PrivateKey
		signer   ed25519.PrivateKey
		want     string
	}{
		{false, keys.source, keys.target, keys.sourceSigner, goldenDiscoveryQueryEnvelope},
		{true, keys.target, keys.source, keys.targetSigner, goldenDiscoveryReplyEnvelope},
	} {
		query := Discovery{RequestID: "query-1", Source: start.Source, TargetMachineID: start.TargetMachineID,
			PairID: start.PairID, Reply: tc.reply, BodyDigest: hex.EncodeToString(sum[:])}
		goldenNonce(t)
		envelope, err := SealDiscovery(query, body, pair, tc.from, tc.signer)
		if err != nil {
			t.Fatal(err)
		}
		pinEnvelope(t, envelope, tc.want)
		var pinned DiscoveryEnvelope
		if err := json.Unmarshal([]byte(tc.want), &pinned); err != nil {
			t.Fatal(err)
		}
		plain, err := OpenDiscovery(pinned, pair, tc.to, tc.signer.Public().(ed25519.PublicKey))
		if err != nil || !bytes.Equal(plain, body) {
			t.Fatalf("pinned discovery reply=%v opened as %q: %v", tc.reply, plain, err)
		}
	}
}

func TestTaskStartEnvelopeBytesArePinned(t *testing.T) {
	keys := newGoldenKeys(t)
	start, body := validTaskStart()
	pair := goldenPair(start.PairID, start.Source.MachineID, start.TargetMachineID)
	goldenNonce(t)
	envelope, err := SealTaskStart(start, body, pair, keys.source, keys.sourceSigner)
	if err != nil {
		t.Fatal(err)
	}
	pinEnvelope(t, envelope, goldenTaskStartEnvelope)
	var pinned TaskStartEnvelope
	if err := json.Unmarshal([]byte(goldenTaskStartEnvelope), &pinned); err != nil {
		t.Fatal(err)
	}
	plain, err := OpenTaskStart(pinned, pair, keys.target, keys.sourceSigner.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(plain, body) {
		t.Fatalf("pinned task start opened as %q: %v", plain, err)
	}
}

func TestTaskEventEnvelopeBytesArePinned(t *testing.T) {
	keys := newGoldenKeys(t)
	start, _ := validTaskStart()
	pair := goldenPair(start.PairID, start.Source.MachineID, start.TargetMachineID)
	targetEndpoint := Endpoint{MachineID: start.TargetMachineID, SessionID: "target-session",
		ExecutionGeneration: "fedcba9876543210fedcba9876543210"}
	for _, tc := range []struct {
		kind              string
		body              []byte
		sender, recipient Endpoint
		from, to          *ecdh.PrivateKey
		signer            ed25519.PrivateKey
		want              string
	}{
		{TaskEventOpened, []byte(`{"state":"opened"}`), targetEndpoint, start.Source,
			keys.target, keys.source, keys.targetSigner, goldenTaskEventFromTargetEnvelope},
		{TaskEventMessage, []byte("Please check one more directory."), start.Source, targetEndpoint,
			keys.source, keys.target, keys.sourceSigner, goldenTaskEventFromSourceEnvelope},
	} {
		sum := sha256.Sum256(tc.body)
		event := TaskEvent{Start: start, RequestID: "event-1", Kind: tc.kind,
			Sender: tc.sender, Recipient: tc.recipient, BodyDigest: hex.EncodeToString(sum[:])}
		goldenNonce(t)
		envelope, err := SealTaskEvent(event, tc.body, pair, tc.from, tc.signer)
		if err != nil {
			t.Fatal(err)
		}
		pinEnvelope(t, envelope, tc.want)
		var pinned TaskEventEnvelope
		if err := json.Unmarshal([]byte(tc.want), &pinned); err != nil {
			t.Fatal(err)
		}
		plain, err := OpenTaskEvent(pinned, pair, tc.to, tc.signer.Public().(ed25519.PublicKey))
		if err != nil || !bytes.Equal(plain, tc.body) {
			t.Fatalf("pinned %s event opened as %q: %v", tc.kind, plain, err)
		}
	}
}

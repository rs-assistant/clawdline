package cloud

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

// identityRun is one pairing up to the activate, with a Go viewer. Every key is
// synthetic and generated here.
type identityRun struct {
	q          IdentityPairingQR
	machineKey DeviceKey
	machineEph []byte
	viewerEph  []byte
	viewerPub  string
	offer      IdentityOffer
	offerW     PairingWrapper
	grantW     PairingWrapper
	master     []byte
}

func mustRand(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func newIdentityRun(t *testing.T) identityRun {
	t.Helper()
	machineKey, err := NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	machineEph, _ := NewX25519Private(nil)
	viewerEph, _ := NewX25519Private(nil)
	mePub, _ := X25519PublicKey(machineEph)
	vePub, _ := X25519PublicKey(viewerEph)
	viewerPub, _, _ := ed25519.GenerateKey(nil)
	q := IdentityPairingQR{
		PairingID: "pai_test", ClaimNonce: base64.StdEncoding.EncodeToString(mustRand(t, 32)), ExpiresAt: 2_000,
		AccountID: "usr_test", MachineID: "mac_test",
		MachineSigningKey:   base64.StdEncoding.EncodeToString(machineKey.PublicKey()),
		MachineFingerprint:  machineKey.Fingerprint(),
		MachineEphemeralKey: base64.StdEncoding.EncodeToString(mePub),
		PairingNonce:        base64.StdEncoding.EncodeToString(mustRand(t, 32)),
	}
	if err := q.Validate(1_000); err != nil {
		t.Fatal(err)
	}
	r := identityRun{q: q, machineKey: machineKey, machineEph: machineEph, viewerEph: viewerEph,
		viewerPub: base64.StdEncoding.EncodeToString(viewerPub), master: mustRand(t, 32)}
	r.offer = IdentityOffer{PairingID: q.PairingID, AccountID: q.AccountID, MachineID: q.MachineID,
		ViewerDeviceID: "wd_test", ViewerSigningKey: r.viewerPub, ViewerFingerprint: Fingerprint(viewerPub),
		ViewerEphKey: base64.StdEncoding.EncodeToString(vePub)}
	r.offerW, err = SealIdentityPhase(q, IdentityPhaseOffer, "wd_test", viewerEph, q.MachineEphemeralKey, mustRand(t, 12), r.offer.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	clear, err := OpenIdentityPhase(q, r.offerW, IdentityPhaseOffer, "wd_test", r.offer.ViewerEphKey, machineEph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIdentityOffer(clear, q, r.offerW); err != nil {
		t.Fatal(err)
	}
	grant := IdentityGrant{PairingID: q.PairingID, AccountID: q.AccountID, MachineID: q.MachineID,
		MachineSigningKey: q.MachineSigningKey, MachineFingerprint: q.MachineFingerprint, ViewerDeviceID: "wd_test",
		OfferSHA256: PhaseDigest(r.offerW), ContentKeyEpoch: 1, IdentityEpoch: 0,
		MasterSecret: base64.StdEncoding.EncodeToString(r.master)}
	r.grantW, err = SealIdentityPhase(q, IdentityPhaseGrant, q.MachineID, machineEph, r.offer.ViewerEphKey, mustRand(t, 12), grant.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// activate is the viewer: it opens the grant only when it can, and confirms
// with whatever secret it holds.
func (r identityRun) activate(t *testing.T, heldMaster []byte) PairingWrapper {
	t.Helper()
	kc, err := KeyConfirmation(heldMaster, KeyConfirmationInput{AccountID: r.q.AccountID, PairingID: r.q.PairingID,
		MachineID: r.q.MachineID, ViewerDeviceID: "wd_test", ViewerSigningKey: r.viewerPub, ContentKeyEpoch: 1,
		IdentityEpoch: 0, GrantSHA256: PhaseDigest(r.grantW)})
	if err != nil {
		t.Fatal(err)
	}
	act := IdentityActivate{PairingID: r.q.PairingID, AccountID: r.q.AccountID, MachineID: r.q.MachineID,
		ViewerDeviceID: "wd_test", ContentKeyEpoch: 1, GrantSHA256: PhaseDigest(r.grantW),
		KeyConfirmation: base64.StdEncoding.EncodeToString(kc)}
	w, err := SealIdentityPhase(r.q, IdentityPhaseActivate, "wd_test", r.viewerEph, r.q.MachineEphemeralKey, mustRand(t, 12), act.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func (r identityRun) check(activate PairingWrapper) ActivateCheck {
	return ActivateCheck{QR: r.q, Offer: r.offer, GrantSHA256: PhaseDigest(r.grantW), ActivateWrapper: activate,
		ActivatePhaseSHA: PhaseDigest(activate), MachineEphemeral: r.machineEph, Master: r.master,
		ContentKeyEpoch: 1, IdentityEpoch: 0, MachineKeyEpoch: 1, ViewerKeyEpoch: 1, ConfirmNonce: make([]byte, 12)}
}

func TestIdentityPairingAttestsOnlyAVerifiedKeyConfirmation(t *testing.T) {
	r := newIdentityRun(t)
	clear, err := OpenIdentityPhase(r.q, r.grantW, IdentityPhaseGrant, r.q.MachineID, r.q.MachineEphemeralKey, r.viewerEph)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := DecodeIdentityGrant(clear, r.q, PhaseDigest(r.offerW), "wd_test")
	if err != nil {
		t.Fatal(err)
	}
	opened, _ := base64.StdEncoding.DecodeString(grant.MasterSecret)
	act := r.activate(t, opened)
	att, err := AttestVerifiedActivate(r.check(act), r.machineKey)
	if err != nil {
		t.Fatalf("a viewer holding the secret is refused: %v", err)
	}
	msg, _ := att.Statement.Message()
	sig, _ := base64.StdEncoding.DecodeString(att.Attestation)
	if !ed25519.Verify(r.machineKey.PublicKey(), msg, sig) {
		t.Fatal("the attestation does not verify against the machine key")
	}
	if att.Statement.ConfirmSHA256 != PhaseDigest(att.Confirm) || att.Statement.ActivateSHA256 != PhaseDigest(act) {
		t.Fatal("the statement does not bind the activate and confirm bytes")
	}
	confirmClear, err := OpenIdentityPhase(r.q, att.Confirm, IdentityPhaseConfirm, r.q.MachineID, r.q.MachineEphemeralKey, r.viewerEph)
	if err != nil {
		t.Fatal(err)
	}
	if err := DecodeIdentityConfirm(confirmClear, IdentityConfirm{PairingID: r.q.PairingID, AccountID: r.q.AccountID,
		MachineID: r.q.MachineID, ViewerDeviceID: "wd_test", ContentKeyEpoch: 1, ActivateSHA256: PhaseDigest(act)}); err != nil {
		t.Fatal(err)
	}
}

// A device that registered and wrote an activate but never opened the grant
// has only a guess at the secret. The machine must not sign for it.
func TestIdentityPairingRegisteredButNeverGotKeyIsNotAttested(t *testing.T) {
	r := newIdentityRun(t)
	act := r.activate(t, mustRand(t, 32))
	if _, err := AttestVerifiedActivate(r.check(act), r.machineKey); !errors.Is(err, ErrKeyConfirmation) {
		t.Fatalf("got %v, want ErrKeyConfirmation", err)
	}
}

func TestIdentityPairingRefusesTamperedActivate(t *testing.T) {
	r := newIdentityRun(t)
	act := r.activate(t, r.master)

	c := r.check(act)
	c.ActivatePhaseSHA = PhaseDigest(r.grantW)
	if _, err := AttestVerifiedActivate(c, r.machineKey); !errors.Is(err, ErrIdentityPhaseDigest) {
		t.Fatalf("digest mismatch: got %v", err)
	}
	c = r.check(act)
	c.GrantSHA256 = PhaseDigest(r.offerW)
	if _, err := AttestVerifiedActivate(c, r.machineKey); !errors.Is(err, ErrIdentityTranscript) {
		t.Fatalf("another grant: got %v", err)
	}
	c = r.check(act)
	c.ContentKeyEpoch = 2
	if _, err := AttestVerifiedActivate(c, r.machineKey); !errors.Is(err, ErrIdentityTranscript) {
		t.Fatalf("another epoch: got %v", err)
	}
	flipped := act
	ct, _ := base64.StdEncoding.DecodeString(act.Ct)
	ct[0] ^= 1
	flipped.Ct = base64.StdEncoding.EncodeToString(ct)
	c = r.check(flipped)
	if _, err := AttestVerifiedActivate(c, r.machineKey); !errors.Is(err, ErrPairingSealed) {
		t.Fatalf("flipped ciphertext: got %v", err)
	}
	// The grant key cannot open an activate: phases are separate keys.
	asGrant := act
	asGrant.Phase = IdentityPhaseGrant
	if _, err := OpenIdentityPhase(r.q, asGrant, IdentityPhaseGrant, "wd_test", r.offer.ViewerEphKey, r.machineEph); !errors.Is(err, ErrPairingSealed) {
		t.Fatalf("cross-phase open: got %v", err)
	}
}

func TestIdentityShortCodeChangesWithTheViewer(t *testing.T) {
	r := newIdentityRun(t)
	a := r.q.ShortCode(r.viewerPub, r.offer.ViewerEphKey)
	other, _, _ := ed25519.GenerateKey(nil)
	b := r.q.ShortCode(base64.StdEncoding.EncodeToString(other), r.offer.ViewerEphKey)
	if len(a) != 6 || a == b {
		t.Fatalf("short codes %q and %q", a, b)
	}
}

func TestContentKeyIDNamesTheSecretWithoutIt(t *testing.T) {
	m := mustRand(t, 32)
	id, err := ContentKeyID(m)
	if err != nil || len(id) != 32 || bytes.Contains([]byte(id), []byte(base64.StdEncoding.EncodeToString(m))) {
		t.Fatalf("id %q err %v", id, err)
	}
	other, _ := ContentKeyID(mustRand(t, 32))
	if other == id {
		t.Fatal("two secrets share an id")
	}
}

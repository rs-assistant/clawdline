package cloud

// Identity pairing: the four-phase handover Cloud's strict routes carry
// (`/v1/pairing/identity/start`, `/phases/:phase`, `/poll`), and the one thing
// those routes cannot see — that the viewer actually holds the account master
// secret afterwards.
//
// The legacy invitation pairing (pairing.go) moves the master secret in one
// `grant` blob and never hears back. Cloud cannot tell a device that received
// the key from one that only registered, so sealed diagnostics stay closed
// (`403 key_handover_required`) until this machine, the only party that knows,
// signs a statement saying so. That statement is worth exactly as much as the
// check behind it, which is why the order below is fixed:
//
//	start     machine → Cloud: registered signing key, fresh X25519 key,
//	          pairing nonce, content-key epoch. Cloud answers with the QR.
//	offer     viewer → machine: who it is (sealed under the offer phase key).
//	grant     machine → viewer: the master secret and its content-key epoch,
//	          only after a person compared the short code on both screens.
//	activate  viewer → machine: a key confirmation — an HMAC under a key
//	          derived from the master secret it just opened, over the whole
//	          transcript. Without the secret it cannot be produced.
//	confirm   machine → viewer: sent only after the confirmation verified,
//	          with `handover_attestation`, the machine's Ed25519 signature over
//	          the statement Cloud rebuilds from its own pairing state.
//
// Every phase is AES-256-GCM under DerivePairingPhaseKey with the phase name as
// info, so a key for `grant` cannot open `activate`. The wrapper and its AAD are
// pairing.go's.

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
)

const (
	IdentityPhaseOffer    = "offer"
	IdentityPhaseGrant    = "grant"
	IdentityPhaseActivate = "activate"
	IdentityPhaseConfirm  = "confirm"

	// IdentityPairingFragmentName is the console fragment that carries the
	// QR: `#pairid=<base64url canonical QR>`. It is a different name from the
	// legacy invitation's `pair` so neither reader mistakes the other.
	IdentityPairingFragmentName = "pairid"

	// HandoverStatementSchema and its context are Cloud's
	// (`api/src/domain/pairing.ts` HANDOVER_ATTESTATION_SCHEMA).
	HandoverStatementSchema    = "clawdline.content-key-grant/1"
	handoverAttestationContext = "clawdline-content-key-grant-v1\n"

	// KeyConfirmationSchema is the activate key confirmation the two
	// clients define; Cloud never sees it.
	KeyConfirmationSchema  = "clawdline.content-key-confirm/1"
	keyConfirmationContext = "clawdline-content-key-confirm-v1\n"
	// KeyConfirmationInfo is the HKDF info of the confirmation key. It is a
	// sibling of the sealed-diagnostics info, never the same key.
	KeyConfirmationInfo = "clawdline/content-key-confirm/v1"

	identityQRType       = "pairing_qr"
	identityOfferType    = "identity_offer"
	identityGrantType    = "identity_grant"
	identityActivateType = "identity_activate"
	identityConfirmType  = "identity_confirm"
	shortCodeContext     = "clawdline-identity-pairing-code-v1\n"
)

var (
	ErrIdentityPhase       = errors.New("the identity pairing phase is not the one expected")
	ErrIdentityTranscript  = errors.New("the identity pairing document does not match this pairing's transcript")
	ErrKeyConfirmation     = errors.New("the viewer's key confirmation did not verify: it does not hold the handed-over secret")
	ErrHandoverStatement   = errors.New("the handover statement is incomplete")
	ErrIdentityEpoch       = errors.New("a content-key epoch must be at least 1")
	ErrIdentityPhaseDigest = errors.New("the phase bytes do not hash to the digest Cloud recorded")
	ErrPairingNotApproved  = errors.New("the short code was not confirmed on this machine")
)

// MARK: QR

// IdentityPairingQR is Cloud's `start` answer, `qr` member, exactly. It is
// what a person carries from this machine to the viewer.
type IdentityPairingQR struct {
	PairingID           string
	ClaimNonce          string
	ExpiresAt           int64
	AccountID           string
	MachineID           string
	MachineSigningKey   string
	MachineFingerprint  string
	MachineEphemeralKey string
	PairingNonce        string
}

var identityQRMembers = []string{
	"account_id", "claim_nonce", "expires_at", "machine_ephemeral_key", "machine_fingerprint",
	"machine_id", "machine_signing_key", "pairing_id", "pairing_nonce", "type", "v",
}

func (q IdentityPairingQR) Value() Value {
	return Object(map[string]Value{
		"v": Int(1), "type": Str(identityQRType),
		"pairing_id": Str(q.PairingID), "claim_nonce": Str(q.ClaimNonce), "expires_at": Int(q.ExpiresAt),
		"account_id": Str(q.AccountID), "machine_id": Str(q.MachineID),
		"machine_signing_key": Str(q.MachineSigningKey), "machine_fingerprint": Str(q.MachineFingerprint),
		"machine_ephemeral_key": Str(q.MachineEphemeralKey), "pairing_nonce": Str(q.PairingNonce),
	})
}

// Fragment is the QR as the console link carries it.
func (q IdentityPairingQR) Fragment() string {
	return EncodeCanonicalBase64URL(CanonicalBytes(q.Value()))
}

// Validate refuses a QR whose keys, nonces or fingerprint do not hold.
func (q IdentityPairingQR) Validate(nowMS int64) error {
	for _, id := range []string{q.PairingID, q.AccountID, q.MachineID} {
		if err := validPairingID(id); err != nil {
			return err
		}
	}
	claim, err := decodeFixedBase64(q.ClaimNonce, PairingNonceBytes)
	if err != nil {
		return err
	}
	pairing, err := decodeFixedBase64(q.PairingNonce, PairingNonceBytes)
	if err != nil {
		return err
	}
	if string(claim) == string(pairing) {
		return ErrPairingNonceReuse
	}
	signing, err := decodeFixedBase64(q.MachineSigningKey, ed25519.PublicKeySize)
	if err != nil {
		return err
	}
	if q.MachineFingerprint != Fingerprint(signing) {
		return ErrPairingFingerprint
	}
	if _, err := decodeFixedBase64(q.MachineEphemeralKey, PairingEphemeralBytes); err != nil {
		return err
	}
	if q.ExpiresAt < nowMS {
		return ErrPairingExpired
	}
	return nil
}

// DecodeIdentityPairingQR reads Cloud's `qr` object or a fragment's bytes.
func DecodeIdentityPairingQR(raw []byte, nowMS int64) (IdentityPairingQR, error) {
	value, err := ParseStrict(raw)
	if err != nil {
		return IdentityPairingQR{}, err
	}
	if err := exactMembers(value, identityQRMembers); err != nil {
		return IdentityPairingQR{}, err
	}
	if err := requireVersionAndType(value, identityQRType); err != nil {
		return IdentityPairingQR{}, err
	}
	expires, ok := memberInt(value, "expires_at")
	if !ok {
		return IdentityPairingQR{}, ErrPairingFields
	}
	q := IdentityPairingQR{
		PairingID: memberString(value, "pairing_id"), ClaimNonce: memberString(value, "claim_nonce"),
		ExpiresAt: expires, AccountID: memberString(value, "account_id"), MachineID: memberString(value, "machine_id"),
		MachineSigningKey: memberString(value, "machine_signing_key"), MachineFingerprint: memberString(value, "machine_fingerprint"),
		MachineEphemeralKey: memberString(value, "machine_ephemeral_key"), PairingNonce: memberString(value, "pairing_nonce"),
	}
	return q, q.Validate(nowMS)
}

// ShortCode is the six digits a person compares on both screens before this
// machine hands over anything. It commits to the pairing, the viewer's
// signing key and both ephemeral keys, so a Cloud that swapped the viewer (or
// its ephemeral key) shows a different code. It is a comparison, not a secret.
func (q IdentityPairingQR) ShortCode(viewerSigningKey, viewerEphemeralKey string) string {
	var pre []byte
	pre = append(pre, shortCodeContext...)
	for _, field := range []string{q.PairingID, q.MachineSigningKey, q.MachineEphemeralKey, viewerSigningKey, viewerEphemeralKey} {
		pre = appendL16(pre, []byte(field))
	}
	sum := sha256.Sum256(pre)
	n := (uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3])) % 1_000_000
	return fmt.Sprintf("%06d", n)
}

// MARK: phase sealing

// PhaseDigest is Cloud's `phase_sha256`: hex SHA-256 of the canonical wrapper.
func PhaseDigest(w PairingWrapper) string {
	sum := sha256.Sum256(w.CanonicalJSON())
	return hex.EncodeToString(sum[:])
}

// SealIdentityPhase seals plaintext for `phase` with this side's ephemeral
// private key against the peer's ephemeral public key.
func SealIdentityPhase(q IdentityPairingQR, phase, senderDeviceID string, ownEphemeralPrivate []byte, peerEphemeral string, nonce, plaintext []byte) (PairingWrapper, error) {
	if len(nonce) != NonceBytes {
		return PairingWrapper{}, ErrNonceLength
	}
	if err := validPairingID(senderDeviceID); err != nil {
		return PairingWrapper{}, err
	}
	key, err := identityPhaseKey(q, phase, ownEphemeralPrivate, peerEphemeral)
	if err != nil {
		return PairingWrapper{}, err
	}
	own, err := X25519PublicKey(ownEphemeralPrivate)
	if err != nil {
		return PairingWrapper{}, err
	}
	w := PairingWrapper{Phase: phase, PairingID: q.PairingID, SenderDeviceID: senderDeviceID,
		EphemeralKey: base64.StdEncoding.EncodeToString(own), Nonce: base64.StdEncoding.EncodeToString(nonce)}
	aad, err := w.AAD()
	if err != nil {
		return PairingWrapper{}, err
	}
	aead, err := ContentKey{raw: key}.aead()
	if err != nil {
		return PairingWrapper{}, err
	}
	w.Ct = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, aad))
	if len(w.CanonicalJSON()) > MaxPairingBodyBytes {
		return PairingWrapper{}, ErrPairingBodyTooBig
	}
	return w, nil
}

// OpenIdentityPhase opens a wrapper Cloud returned for `phase`, refusing one
// written by another device or bound to another ephemeral key.
func OpenIdentityPhase(q IdentityPairingQR, w PairingWrapper, phase, wantSender, wantPeerEphemeral string, ownEphemeralPrivate []byte) ([]byte, error) {
	if w.Phase != phase {
		return nil, ErrIdentityPhase
	}
	if w.PairingID != q.PairingID || w.SenderDeviceID != wantSender {
		return nil, ErrPairingSender
	}
	if w.EphemeralKey != wantPeerEphemeral {
		return nil, ErrPairingKeyBinding
	}
	key, err := identityPhaseKey(q, phase, ownEphemeralPrivate, w.EphemeralKey)
	if err != nil {
		return nil, err
	}
	aad, err := w.AAD()
	if err != nil {
		return nil, err
	}
	nonce, err := decodeFixedBase64(w.Nonce, NonceBytes)
	if err != nil {
		return nil, err
	}
	ct, err := DecodeCanonicalBase64(w.Ct)
	if err != nil {
		return nil, err
	}
	aead, err := ContentKey{raw: key}.aead()
	if err != nil {
		return nil, err
	}
	clear, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrPairingSealed
	}
	return clear, nil
}

func identityPhaseKey(q IdentityPairingQR, phase string, ownPrivate []byte, peerEphemeral string) ([]byte, error) {
	switch phase {
	case IdentityPhaseOffer, IdentityPhaseGrant, IdentityPhaseActivate, IdentityPhaseConfirm:
	default:
		return nil, ErrIdentityPhase
	}
	peer, err := decodeFixedBase64(peerEphemeral, PairingEphemeralBytes)
	if err != nil {
		return nil, err
	}
	shared, err := PairingSharedSecret(ownPrivate, peer)
	if err != nil {
		return nil, err
	}
	pn, err := decodeFixedBase64(q.PairingNonce, PairingNonceBytes)
	if err != nil {
		return nil, err
	}
	cn, err := decodeFixedBase64(q.ClaimNonce, PairingNonceBytes)
	if err != nil {
		return nil, err
	}
	return DerivePairingPhaseKey(shared, pn, q.PairingID, cn, phase)
}

// MARK: phase documents

// IdentityOffer is the viewer's `offer` plaintext.
type IdentityOffer struct {
	PairingID, AccountID, MachineID                                   string
	ViewerDeviceID, ViewerSigningKey, ViewerFingerprint, ViewerEphKey string
}

var identityOfferMembers = []string{
	"account_id", "machine_id", "pairing_id", "type", "v", "viewer_device_id",
	"viewer_ephemeral_key", "viewer_fingerprint", "viewer_signing_key",
}

func (o IdentityOffer) Canonical() []byte {
	return CanonicalBytes(Object(map[string]Value{
		"v": Int(1), "type": Str(identityOfferType), "pairing_id": Str(o.PairingID),
		"account_id": Str(o.AccountID), "machine_id": Str(o.MachineID),
		"viewer_device_id": Str(o.ViewerDeviceID), "viewer_signing_key": Str(o.ViewerSigningKey),
		"viewer_fingerprint": Str(o.ViewerFingerprint), "viewer_ephemeral_key": Str(o.ViewerEphKey),
	}))
}

// DecodeIdentityOffer reads an opened offer and checks it against the QR and
// the wrapper it arrived in.
func DecodeIdentityOffer(clear []byte, q IdentityPairingQR, w PairingWrapper) (IdentityOffer, error) {
	value, err := strictDocument(clear, identityOfferMembers, identityOfferType)
	if err != nil {
		return IdentityOffer{}, err
	}
	o := IdentityOffer{
		PairingID: memberString(value, "pairing_id"), AccountID: memberString(value, "account_id"),
		MachineID: memberString(value, "machine_id"), ViewerDeviceID: memberString(value, "viewer_device_id"),
		ViewerSigningKey: memberString(value, "viewer_signing_key"), ViewerFingerprint: memberString(value, "viewer_fingerprint"),
		ViewerEphKey: memberString(value, "viewer_ephemeral_key"),
	}
	if o.PairingID != q.PairingID || o.AccountID != q.AccountID || o.MachineID != q.MachineID ||
		o.ViewerDeviceID != w.SenderDeviceID || o.ViewerEphKey != w.EphemeralKey {
		return IdentityOffer{}, ErrIdentityTranscript
	}
	signing, err := decodeFixedBase64(o.ViewerSigningKey, ed25519.PublicKeySize)
	if err != nil {
		return IdentityOffer{}, err
	}
	if o.ViewerFingerprint != Fingerprint(signing) {
		return IdentityOffer{}, ErrPairingFingerprint
	}
	return o, nil
}

// IdentityGrant is the machine's `grant` plaintext: the account master secret
// and the content-key epoch it is, bound to the offer it answers.
type IdentityGrant struct {
	PairingID, AccountID, MachineID, MachineSigningKey, MachineFingerprint string
	ViewerDeviceID, OfferSHA256                                            string
	ContentKeyEpoch, IdentityEpoch                                         int64
	MasterSecret                                                           string
}

var identityGrantMembers = []string{
	"account_id", "content_key_epoch", "identity_epoch", "machine_fingerprint", "machine_id",
	"machine_signing_key", "master_secret", "offer_sha256", "pairing_id", "type", "v", "viewer_device_id",
}

func (g IdentityGrant) Canonical() []byte {
	return CanonicalBytes(Object(map[string]Value{
		"v": Int(1), "type": Str(identityGrantType), "pairing_id": Str(g.PairingID),
		"account_id": Str(g.AccountID), "machine_id": Str(g.MachineID),
		"machine_signing_key": Str(g.MachineSigningKey), "machine_fingerprint": Str(g.MachineFingerprint),
		"viewer_device_id": Str(g.ViewerDeviceID), "offer_sha256": Str(g.OfferSHA256),
		"content_key_epoch": Int(g.ContentKeyEpoch), "identity_epoch": Int(g.IdentityEpoch),
		"master_secret": Str(g.MasterSecret),
	}))
}

// DecodeIdentityGrant is the viewer half in Go; the production viewer is the
// browser mirror in cloud-pairing.js. It exists for the round-trip tests.
func DecodeIdentityGrant(clear []byte, q IdentityPairingQR, offerSHA256, viewerDeviceID string) (IdentityGrant, error) {
	value, err := strictDocument(clear, identityGrantMembers, identityGrantType)
	if err != nil {
		return IdentityGrant{}, err
	}
	ce, ok1 := memberInt(value, "content_key_epoch")
	ie, ok2 := memberInt(value, "identity_epoch")
	if !ok1 || !ok2 {
		return IdentityGrant{}, ErrPairingFields
	}
	g := IdentityGrant{
		PairingID: memberString(value, "pairing_id"), AccountID: memberString(value, "account_id"),
		MachineID: memberString(value, "machine_id"), MachineSigningKey: memberString(value, "machine_signing_key"),
		MachineFingerprint: memberString(value, "machine_fingerprint"), ViewerDeviceID: memberString(value, "viewer_device_id"),
		OfferSHA256: memberString(value, "offer_sha256"), ContentKeyEpoch: ce, IdentityEpoch: ie,
		MasterSecret: memberString(value, "master_secret"),
	}
	if g.PairingID != q.PairingID || g.AccountID != q.AccountID || g.MachineID != q.MachineID ||
		g.MachineSigningKey != q.MachineSigningKey || g.MachineFingerprint != q.MachineFingerprint ||
		g.ViewerDeviceID != viewerDeviceID || g.OfferSHA256 != offerSHA256 {
		return IdentityGrant{}, ErrIdentityTranscript
	}
	if g.ContentKeyEpoch < 1 || g.IdentityEpoch < 0 {
		return IdentityGrant{}, ErrIdentityEpoch
	}
	if _, err := decodeFixedBase64(g.MasterSecret, ContentKeyBytes); err != nil {
		return IdentityGrant{}, err
	}
	return g, nil
}

// KeyConfirmationInput is everything the activate confirmation commits to.
type KeyConfirmationInput struct {
	AccountID, PairingID, MachineID, ViewerDeviceID, ViewerSigningKey string
	ContentKeyEpoch, IdentityEpoch                                    int64
	GrantSHA256                                                       string
}

func (k KeyConfirmationInput) message() []byte {
	return append([]byte(keyConfirmationContext), CanonicalBytes(Object(map[string]Value{
		"schema": Str(KeyConfirmationSchema), "account_id": Str(k.AccountID), "pairing_id": Str(k.PairingID),
		"machine_id": Str(k.MachineID), "viewer_device_id": Str(k.ViewerDeviceID),
		"viewer_signing_key": Str(k.ViewerSigningKey), "content_key_epoch": Int(k.ContentKeyEpoch),
		"identity_epoch": Int(k.IdentityEpoch), "grant_sha256": Str(k.GrantSHA256),
	}))...)
}

// KeyConfirmation is HMAC-SHA256(HKDF-SHA256(master, zero salt,
// KeyConfirmationInfo), transcript). Only a holder of the master secret can
// compute it; the transcript binds it to this pairing's grant.
func KeyConfirmation(master []byte, in KeyConfirmationInput) ([]byte, error) {
	if len(master) != ContentKeyBytes {
		return nil, ErrKeyLength
	}
	key, err := hkdf.Key(sha256.New, master, make([]byte, 32), KeyConfirmationInfo, 32)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(in.message())
	return mac.Sum(nil), nil
}

// VerifyKeyConfirmation answers ErrKeyConfirmation unless confirmation is the
// exact HMAC this machine computes from its own copy of the secret.
func VerifyKeyConfirmation(master []byte, in KeyConfirmationInput, confirmation string) error {
	got, err := decodeFixedBase64(confirmation, sha256.Size)
	if err != nil {
		return ErrKeyConfirmation
	}
	want, err := KeyConfirmation(master, in)
	if err != nil {
		return err
	}
	if !hmac.Equal(got, want) {
		return ErrKeyConfirmation
	}
	return nil
}

// IdentityActivate is the viewer's `activate` plaintext.
type IdentityActivate struct {
	PairingID, AccountID, MachineID, ViewerDeviceID string
	ContentKeyEpoch                                 int64
	GrantSHA256, KeyConfirmation                    string
}

var identityActivateMembers = []string{
	"account_id", "content_key_epoch", "grant_sha256", "key_confirmation", "machine_id",
	"pairing_id", "type", "v", "viewer_device_id",
}

func (a IdentityActivate) Canonical() []byte {
	return CanonicalBytes(Object(map[string]Value{
		"v": Int(1), "type": Str(identityActivateType), "pairing_id": Str(a.PairingID),
		"account_id": Str(a.AccountID), "machine_id": Str(a.MachineID), "viewer_device_id": Str(a.ViewerDeviceID),
		"content_key_epoch": Int(a.ContentKeyEpoch), "grant_sha256": Str(a.GrantSHA256),
		"key_confirmation": Str(a.KeyConfirmation),
	}))
}

// DecodeIdentityActivate reads an opened activate and checks the transcript
// fields this machine already knows. The confirmation itself is checked by
// VerifyKeyConfirmation with the secret.
func DecodeIdentityActivate(clear []byte, q IdentityPairingQR, viewerDeviceID string, contentKeyEpoch int64, grantSHA256 string) (IdentityActivate, error) {
	value, err := strictDocument(clear, identityActivateMembers, identityActivateType)
	if err != nil {
		return IdentityActivate{}, err
	}
	ce, ok := memberInt(value, "content_key_epoch")
	if !ok {
		return IdentityActivate{}, ErrPairingFields
	}
	a := IdentityActivate{
		PairingID: memberString(value, "pairing_id"), AccountID: memberString(value, "account_id"),
		MachineID: memberString(value, "machine_id"), ViewerDeviceID: memberString(value, "viewer_device_id"),
		ContentKeyEpoch: ce, GrantSHA256: memberString(value, "grant_sha256"),
		KeyConfirmation: memberString(value, "key_confirmation"),
	}
	if a.PairingID != q.PairingID || a.AccountID != q.AccountID || a.MachineID != q.MachineID ||
		a.ViewerDeviceID != viewerDeviceID || a.ContentKeyEpoch != contentKeyEpoch || a.GrantSHA256 != grantSHA256 {
		return IdentityActivate{}, ErrIdentityTranscript
	}
	return a, nil
}

// IdentityConfirm is the machine's `confirm` plaintext: the viewer learns the
// machine accepted its activate, by digest.
type IdentityConfirm struct {
	PairingID, AccountID, MachineID, ViewerDeviceID string
	ContentKeyEpoch                                 int64
	ActivateSHA256                                  string
}

var identityConfirmMembers = []string{
	"account_id", "activate_sha256", "content_key_epoch", "machine_id", "pairing_id", "type", "v", "viewer_device_id",
}

func (c IdentityConfirm) Canonical() []byte {
	return CanonicalBytes(Object(map[string]Value{
		"v": Int(1), "type": Str(identityConfirmType), "pairing_id": Str(c.PairingID),
		"account_id": Str(c.AccountID), "machine_id": Str(c.MachineID), "viewer_device_id": Str(c.ViewerDeviceID),
		"content_key_epoch": Int(c.ContentKeyEpoch), "activate_sha256": Str(c.ActivateSHA256),
	}))
}

// DecodeIdentityConfirm is the viewer half in Go, for the round-trip tests.
func DecodeIdentityConfirm(clear []byte, want IdentityConfirm) error {
	value, err := strictDocument(clear, identityConfirmMembers, identityConfirmType)
	if err != nil {
		return err
	}
	ce, _ := memberInt(value, "content_key_epoch")
	got := IdentityConfirm{
		PairingID: memberString(value, "pairing_id"), AccountID: memberString(value, "account_id"),
		MachineID: memberString(value, "machine_id"), ViewerDeviceID: memberString(value, "viewer_device_id"),
		ContentKeyEpoch: ce, ActivateSHA256: memberString(value, "activate_sha256"),
	}
	if got != want {
		return ErrIdentityTranscript
	}
	return nil
}

// MARK: handover statement

// HandoverStatement is Cloud's statement (`api/src/domain/pairing.ts`
// HandoverStatement). Cloud rebuilds every field from its pairing state; a
// field this machine got wrong makes the signature fail there, never a grant
// for the wrong device.
type HandoverStatement struct {
	AccountID, PairingID, MachineID  string
	MachineKeyEpoch                  int64
	ViewerDeviceID, ViewerSigningKey string
	ViewerKeyEpoch                   int64
	IdentityEpoch, ContentKeyEpoch   int64
	ActivateSHA256, ConfirmSHA256    string
}

// Message is the exact signed bytes.
func (s HandoverStatement) Message() ([]byte, error) {
	if s.AccountID == "" || s.PairingID == "" || s.MachineID == "" || s.ViewerDeviceID == "" ||
		s.ViewerSigningKey == "" || len(s.ActivateSHA256) != 64 || len(s.ConfirmSHA256) != 64 ||
		s.MachineKeyEpoch < 1 || s.ViewerKeyEpoch < 1 || s.ContentKeyEpoch < 1 || s.IdentityEpoch < 0 {
		return nil, ErrHandoverStatement
	}
	return append([]byte(handoverAttestationContext), CanonicalBytes(Object(map[string]Value{
		"schema": Str(HandoverStatementSchema), "account_id": Str(s.AccountID), "pairing_id": Str(s.PairingID),
		"machine_id": Str(s.MachineID), "machine_key_epoch": Int(s.MachineKeyEpoch),
		"viewer_device_id": Str(s.ViewerDeviceID), "viewer_signing_key": Str(s.ViewerSigningKey),
		"viewer_key_epoch": Int(s.ViewerKeyEpoch), "identity_epoch": Int(s.IdentityEpoch),
		"content_key_epoch": Int(s.ContentKeyEpoch), "activate_sha256": Str(s.ActivateSHA256),
		"confirm_sha256": Str(s.ConfirmSHA256),
	}))...), nil
}

// SignHandover is the attestation, base64. Callers reach it only through
// AttestVerifiedActivate, which is what makes the signature mean something.
func signHandover(key DeviceKey, s HandoverStatement) (string, error) {
	if !key.Valid() {
		return "", ErrKeyLength
	}
	msg, err := s.Message()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key.Sign(msg)), nil
}

// AttestVerifiedActivate is the only way to a handover attestation: it opens
// the activate Cloud returned, checks its digest against Cloud's
// `phase_sha256`, checks the transcript, verifies the key confirmation with
// this machine's own copy of the secret, seals the confirm, and only then
// signs. A viewer that registered but never opened the grant cannot pass.
type ActivateCheck struct {
	QR               IdentityPairingQR
	Offer            IdentityOffer
	GrantSHA256      string
	ActivateWrapper  PairingWrapper
	ActivatePhaseSHA string
	MachineEphemeral []byte
	Master           []byte
	ContentKeyEpoch  int64
	IdentityEpoch    int64
	MachineKeyEpoch  int64
	ViewerKeyEpoch   int64
	ConfirmNonce     []byte
}

// Attested is a confirm wrapper and the signature to send with it.
type Attested struct {
	Confirm     PairingWrapper
	Attestation string
	Statement   HandoverStatement
}

func AttestVerifiedActivate(c ActivateCheck, machineKey DeviceKey) (Attested, error) {
	digest := PhaseDigest(c.ActivateWrapper)
	if digest != c.ActivatePhaseSHA {
		return Attested{}, ErrIdentityPhaseDigest
	}
	clear, err := OpenIdentityPhase(c.QR, c.ActivateWrapper, IdentityPhaseActivate, c.Offer.ViewerDeviceID, c.Offer.ViewerEphKey, c.MachineEphemeral)
	if err != nil {
		return Attested{}, err
	}
	act, err := DecodeIdentityActivate(clear, c.QR, c.Offer.ViewerDeviceID, c.ContentKeyEpoch, c.GrantSHA256)
	if err != nil {
		return Attested{}, err
	}
	if err := VerifyKeyConfirmation(c.Master, KeyConfirmationInput{
		AccountID: c.QR.AccountID, PairingID: c.QR.PairingID, MachineID: c.QR.MachineID,
		ViewerDeviceID: c.Offer.ViewerDeviceID, ViewerSigningKey: c.Offer.ViewerSigningKey,
		ContentKeyEpoch: c.ContentKeyEpoch, IdentityEpoch: c.IdentityEpoch, GrantSHA256: c.GrantSHA256,
	}, act.KeyConfirmation); err != nil {
		return Attested{}, err
	}
	confirm, err := SealIdentityPhase(c.QR, IdentityPhaseConfirm, c.QR.MachineID, c.MachineEphemeral, c.Offer.ViewerEphKey, c.ConfirmNonce,
		IdentityConfirm{PairingID: c.QR.PairingID, AccountID: c.QR.AccountID, MachineID: c.QR.MachineID,
			ViewerDeviceID: c.Offer.ViewerDeviceID, ContentKeyEpoch: c.ContentKeyEpoch, ActivateSHA256: digest}.Canonical())
	if err != nil {
		return Attested{}, err
	}
	st := HandoverStatement{
		AccountID: c.QR.AccountID, PairingID: c.QR.PairingID, MachineID: c.QR.MachineID, MachineKeyEpoch: c.MachineKeyEpoch,
		ViewerDeviceID: c.Offer.ViewerDeviceID, ViewerSigningKey: c.Offer.ViewerSigningKey, ViewerKeyEpoch: c.ViewerKeyEpoch,
		IdentityEpoch: c.IdentityEpoch, ContentKeyEpoch: c.ContentKeyEpoch, ActivateSHA256: digest, ConfirmSHA256: PhaseDigest(confirm),
	}
	sig, err := signHandover(machineKey, st)
	if err != nil {
		return Attested{}, err
	}
	return Attested{Confirm: confirm, Attestation: sig, Statement: st}, nil
}

// MARK: content-key epoch

// ContentKeyID names a master secret without revealing it: hex of the first
// 16 bytes of HKDF(master, zero salt, "clawdline/content-key-id/v1"). It is
// what ties a recorded epoch to the secret it was recorded for, so a replaced
// secret never inherits an epoch.
func ContentKeyID(master []byte) (string, error) {
	if len(master) != ContentKeyBytes {
		return "", ErrKeyLength
	}
	id, err := hkdf.Key(sha256.New, master, make([]byte, 32), "clawdline/content-key-id/v1", 16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(id), nil
}

// IdentityStartBody is the canonical `start` request for a content-key epoch.
// Epoch 1 is the ordinary first handover; a later epoch is a rotation and
// names it.
func IdentityStartBody(machineKey DeviceKey, machineEphemeralPublic, pairingNonce []byte, contentKeyEpoch int64, rotationID string) ([]byte, error) {
	if contentKeyEpoch < 1 {
		return nil, ErrIdentityEpoch
	}
	if contentKeyEpoch == 1 {
		rotationID = ""
	} else if rotationID == "" {
		rotationID = "ck-" + strconv.FormatInt(contentKeyEpoch, 10)
	}
	return CanonicalBytes(Object(map[string]Value{
		"v": Int(1), "machine_signing_key": Str(base64.StdEncoding.EncodeToString(machineKey.PublicKey())),
		"machine_fingerprint": Str(machineKey.Fingerprint()), "machine_ephemeral_key": Str(base64.StdEncoding.EncodeToString(machineEphemeralPublic)),
		"pairing_nonce":              Str(base64.StdEncoding.EncodeToString(pairingNonce)),
		"previous_content_key_epoch": Int(contentKeyEpoch - 1), "content_key_epoch": Int(contentKeyEpoch),
		"rotation_id": Str(rotationID),
	})), nil
}

// NewX25519Private draws an ephemeral private key. Pass nil for crypto/rand.
func NewX25519Private(r io.Reader) ([]byte, error) {
	if r == nil {
		r = rand.Reader
	}
	raw := make([]byte, PairingEphemeralBytes)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, err
	}
	if _, err := X25519PublicKey(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func strictDocument(clear []byte, members []string, kind string) (Value, error) {
	if len(clear) > MaxPairingBodyBytes {
		return Value{}, ErrPairingBodyTooBig
	}
	value, err := ParseStrict(clear)
	if err != nil {
		return Value{}, err
	}
	if err := exactMembers(value, members); err != nil {
		return Value{}, err
	}
	if err := requireVersionAndType(value, kind); err != nil {
		return Value{}, err
	}
	if string(CanonicalBytes(value)) != string(clear) {
		return Value{}, ErrIdentityTranscript
	}
	return value, nil
}

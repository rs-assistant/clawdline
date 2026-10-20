// Package healthsealed seals owner diagnostic snapshots for Cloud's
// owner-sealed attachment store (Cloud's docs/connection-health-contract.md
// §13) and opens what a paired owner reads back.
//
// The only key is the account master secret this machine already holds
// (internal/adapters/cloudkeys); a dedicated subkey is derived from it with
// HKDF-SHA256 so a diagnostic ciphertext can never be mistaken for a relay
// envelope. Cloud only ever sees ciphertext, the 13 AAD labels and digests.
// Nothing here distributes, wraps or invents a key: a machine that cannot
// name the content-key epoch of its master secret does not upload
// (CodeKeyHandoverRequired), and a reader that does not hold the master
// secret of the attachment's epoch cannot open it.
//
// Every cryptographic failure is a typed Error; nothing returns a partial
// plaintext.
package healthsealed

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
)

// Wire names, from §13.2.
const (
	AttachmentSchema = "clawdline.health-sealed-attachment/1"
	AADSchema        = "clawdline.health-sealed-aad/1"
	ReceiptSchema    = "clawdline.health-sealed-receipt/1"
	ReadSchema       = "clawdline.health-sealed-read-response/1"
	Alg              = "A256GCM-HKDF-SHA256"
	HKDFInfo         = "clawdline/health-sealed/v1"

	SubjectIncident = "incident"
	SubjectEvidence = "evidence"

	NonceBytes = 12
	TagBytes   = 16
	// safeIntegerCeiling is the largest integer RFC 8785 canonical JSON (and a
	// JavaScript reader) carries exactly.
	safeIntegerCeiling = 1<<53 - 1
)

// Bounds. Each is registered in internal/domain/capacity
// (register_healthsealed.go). They mirror Cloud's SEALED_LIMITS so the
// machine stops itself before Cloud refuses it.
const (
	// MaxCiphertextBytes is ciphertext||tag of one revision.
	MaxCiphertextBytes = 256 << 10
	// MaxRequestBytes is Cloud's upload body limit.
	MaxRequestBytes = 400 << 10
	// MaxRevisions is how many immutable revisions one attachment holds.
	MaxRevisions = 8
	// MaxLifeSeconds is an attachment's whole life from its first revision.
	MaxLifeSeconds = 7 * 24 * 60 * 60
)

// minLifeMS is Cloud's lower expiry bound (now + 60 s).
const minLifeMS = 60 * 1000

// Code is a stable failure or state a caller can branch on.
type Code string

const (
	CodeDisabled            Code = "owner_retention_off"
	CodeKeyHandoverRequired Code = "key_handover_required"
	CodeKeyUnavailable      Code = "key_unavailable"
	CodeBadInput            Code = "bad_input"
	CodeTooLarge            Code = "too_large"
	CodeTagMismatch         Code = "sealed_tag_mismatch"
	CodeAADMismatch         Code = "aad_mismatch"
	CodeHashMismatch        Code = "hash_mismatch"
	CodeWrongEpoch          Code = "wrong_epoch"
	CodeWrongAccount        Code = "wrong_account"
	CodeWrongMachine        Code = "wrong_machine"
	CodeWrongSubject        Code = "wrong_subject"
	CodeNonceReused         Code = "nonce_reused"
	CodeRevisionConflict    Code = "revision_conflict"
	CodeRevisionsExhausted  Code = "revisions_exhausted"
	CodeExpired             Code = "sealed_expired"
	CodeDeleted             Code = "sealed_deleted"
	CodeStoreFull           Code = "store_full"
	CodeStoreUnwritable     Code = "store_unwritable"
	CodeStoreUnreadable     Code = "store_unreadable"
	CodeBusy                Code = "busy"
	CodeAuthRefused         Code = "auth_refused"
	CodeIdentityChanged     Code = "identity_epoch_changed"
	CodeEpochRegressed      Code = "content_key_epoch_regressed"
	CodeQuota               Code = "sealed_quota_exceeded"
	CodeUpstream            Code = "upstream_unavailable"
	CodeRefused             Code = "refused"
	CodeReceiptMismatch     Code = "receipt_mismatch"
)

// Error carries a Code and, for a Cloud refusal, Cloud's own code. It never
// carries key material, plaintext or a message Cloud wrote.
type Error struct {
	Code  Code
	Cloud string
}

func (e *Error) Error() string {
	if e.Cloud != "" {
		return "healthsealed: " + string(e.Code) + " (" + e.Cloud + ")"
	}
	return "healthsealed: " + string(e.Code)
}

// CodeOf is err's code, or "" for an error this package did not make.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func fail(c Code) error { return &Error{Code: c} }

// DeriveKey is K = HKDF-SHA256(IKM = master secret, salt = 32 zero bytes,
// info = HKDFInfo, L = 32).
func DeriveKey(master []byte) ([]byte, error) {
	if len(master) != 32 {
		return nil, fail(CodeKeyUnavailable)
	}
	return hkdf.Key(sha256.New, master, make([]byte, 32), HKDFInfo, 32)
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// AAD is exactly the 13 fields §13.2 binds. The field order here is the
// canonical (sorted) order, which Canonical relies on.
type AAD struct {
	Alg                      string `json:"alg"`
	AttachmentID             string `json:"attachment_id"`
	BootID                   string `json:"boot_id"`
	ContentKeyEpoch          int64  `json:"content_key_epoch"`
	ExpiresAtMS              int64  `json:"expires_at_ms"`
	IdentityEpoch            int64  `json:"identity_epoch"`
	MachineID                string `json:"machine_id"`
	PreviousCiphertextSHA256 string `json:"previous_ciphertext_sha256"`
	Revision                 int64  `json:"revision"`
	Schema                   string `json:"schema"`
	SubjectID                string `json:"subject_id"`
	SubjectKind              string `json:"subject_kind"`
	Tenant                   string `json:"tenant"`
}

// Validate refuses an AAD Cloud would refuse or that canonical JSON cannot
// carry exactly.
func (a AAD) Validate() error {
	if a.Alg != Alg || a.Schema != AADSchema {
		return fail(CodeBadInput)
	}
	if !idPattern.MatchString(a.BootID) || !idPattern.MatchString(a.SubjectID) {
		return fail(CodeBadInput)
	}
	if a.SubjectKind != SubjectIncident && a.SubjectKind != SubjectEvidence {
		return fail(CodeBadInput)
	}
	if a.Tenant == "" || a.MachineID == "" || len(a.Tenant) > 128 || len(a.MachineID) > 128 {
		return fail(CodeBadInput)
	}
	if a.AttachmentID != AttachmentID(a.Tenant, a.MachineID, a.BootID, a.SubjectKind, a.SubjectID) {
		return fail(CodeAADMismatch)
	}
	if a.Revision < 1 || a.Revision > MaxRevisions {
		return fail(CodeRevisionsExhausted)
	}
	if (a.Revision == 1) != (a.PreviousCiphertextSHA256 == "") {
		return fail(CodeBadInput)
	}
	if a.PreviousCiphertextSHA256 != "" && !isHexDigest(a.PreviousCiphertextSHA256) {
		return fail(CodeBadInput)
	}
	for _, n := range []int64{a.ContentKeyEpoch, a.ExpiresAtMS, a.IdentityEpoch} {
		if n < 0 || n > safeIntegerCeiling {
			return fail(CodeBadInput)
		}
	}
	if a.ContentKeyEpoch < 1 {
		return fail(CodeWrongEpoch)
	}
	return nil
}

// Canonical is the RFC 8785 canonical JSON of the AAD: sorted keys, no
// whitespace, no HTML escaping. Every string is from a restricted alphabet
// or a hex digest, so the encoder's string form is the canonical one.
func (a AAD) Canonical() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(a); err != nil {
		return nil, fail(CodeBadInput)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Digest is hex SHA-256 of the canonical AAD bytes, which Cloud's receipt
// and read response name as aad_sha256.
func (a AAD) Digest() (string, error) {
	raw, err := a.Canonical()
	if err != nil {
		return "", err
	}
	return hexSHA(raw), nil
}

// AttachmentID is "hs_" + the first 32 hex digits of SHA-256 over the
// subject's identity, as Cloud derives it.
func AttachmentID(tenant, machine, boot, kind, subject string) string {
	sum := sha256.Sum256([]byte(AADSchema + "\n" + tenant + "\n" + machine + "\n" + boot + "\n" + kind + "\n" + subject))
	return "hs_" + hex.EncodeToString(sum[:])[:32]
}

// Seal encrypts plaintext under key with aad's canonical bytes as AES-GCM
// additional data. It returns ciphertext||tag.
func Seal(key []byte, aad AAD, nonce, plaintext []byte) ([]byte, error) {
	gcm, err := aead(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceBytes {
		return nil, fail(CodeBadInput)
	}
	ad, err := aad.Canonical()
	if err != nil {
		return nil, err
	}
	out := gcm.Seal(nil, nonce, plaintext, ad)
	if len(out) > MaxCiphertextBytes {
		return nil, fail(CodeTooLarge)
	}
	return out, nil
}

// Open authenticates and decrypts ciphertext||tag. Any change to the AAD,
// nonce, ciphertext or key is CodeTagMismatch and no plaintext.
func Open(key []byte, aad AAD, nonce, ciphertext []byte) ([]byte, error) {
	gcm, err := aead(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceBytes || len(ciphertext) < TagBytes+1 || len(ciphertext) > MaxCiphertextBytes {
		return nil, fail(CodeBadInput)
	}
	ad, err := aad.Canonical()
	if err != nil {
		return nil, err
	}
	clear, err := gcm.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		return nil, fail(CodeTagMismatch)
	}
	return clear, nil
}

func aead(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fail(CodeKeyUnavailable)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fail(CodeKeyUnavailable)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fail(CodeKeyUnavailable)
	}
	return gcm, nil
}

func hexSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func isHexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

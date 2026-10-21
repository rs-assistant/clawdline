package healthsealed

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
)

// UploadBody is exactly the §13.2 request: nothing else is ever sent, so no
// plaintext, location or key can leave the machine outside the ciphertext.
type UploadBody struct {
	Schema                   string `json:"schema"`
	Alg                      string `json:"alg"`
	BootID                   string `json:"boot_id"`
	SubjectKind              string `json:"subject_kind"`
	SubjectID                string `json:"subject_id"`
	Revision                 int64  `json:"revision"`
	PreviousCiphertextSHA256 string `json:"previous_ciphertext_sha256"`
	ContentKeyEpoch          int64  `json:"content_key_epoch"`
	IdentityEpoch            int64  `json:"identity_epoch"`
	ExpiresAtMS              int64  `json:"expires_at_ms"`
	Nonce                    string `json:"nonce"`
	Ciphertext               string `json:"ciphertext"`
	CiphertextSHA256         string `json:"ciphertext_sha256"`
}

// Receipt is Cloud's answer to an upload (201 stored, 200 duplicate).
type Receipt struct {
	Schema           string `json:"schema"`
	Status           string `json:"status"`
	AttachmentID     string `json:"attachment_id"`
	Revision         int64  `json:"revision"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
	CiphertextBytes  int64  `json:"ciphertext_bytes"`
	AADSHA256        string `json:"aad_sha256"`
	ContentKeyEpoch  int64  `json:"content_key_epoch"`
	IdentityEpoch    int64  `json:"identity_epoch"`
	MachineKeyEpoch  int64  `json:"machine_key_epoch"`
	ReceivedAtMS     int64  `json:"received_at_ms"`
	ExpiresAtMS      int64  `json:"expires_at_ms"`
}

// ReadResponse is §13.3's answer to a viewer proof.
type ReadResponse struct {
	Schema           string `json:"schema"`
	AttachmentID     string `json:"attachment_id"`
	Revision         int64  `json:"revision"`
	AAD              AAD    `json:"aad"`
	AADSHA256        string `json:"aad_sha256"`
	Alg              string `json:"alg"`
	Nonce            string `json:"nonce"`
	Ciphertext       string `json:"ciphertext"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
	ReceivedAtMS     int64  `json:"received_at_ms"`
	Audit            struct {
		Recorded bool `json:"recorded"`
	} `json:"audit"`
}

// Expect is what a reader asked for and who it is: the response must name
// exactly this, or it is not opened.
type Expect struct {
	Tenant       string
	AttachmentID string
	Revision     int64
}

// MasterFor returns the master secret this reader holds for one content-key
// epoch, or ok=false when it holds none for that epoch. It must never fall
// back to another epoch's secret.
type MasterFor func(contentKeyEpoch int64) (secret []byte, ok bool)

// OpenReadResponse checks a read response against what was asked and opens
// it. Every mismatch is a typed error and no plaintext.
func OpenReadResponse(raw []byte, want Expect, master MasterFor) (Snapshot, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var r ReadResponse
	if err := dec.Decode(&r); err != nil {
		return Snapshot{}, fail(CodeBadInput)
	}
	if r.Schema != ReadSchema || r.Alg != Alg || r.AAD.Alg != Alg {
		return Snapshot{}, fail(CodeBadInput)
	}
	if !r.Audit.Recorded {
		return Snapshot{}, fail(CodeRefused)
	}
	digest, err := r.AAD.Digest()
	if err != nil {
		return Snapshot{}, err
	}
	if digest != r.AADSHA256 {
		return Snapshot{}, fail(CodeAADMismatch)
	}
	if r.AAD.Tenant != want.Tenant {
		return Snapshot{}, fail(CodeWrongAccount)
	}
	if r.AttachmentID != want.AttachmentID || r.AAD.AttachmentID != want.AttachmentID ||
		r.Revision != want.Revision || r.AAD.Revision != want.Revision {
		return Snapshot{}, fail(CodeWrongSubject)
	}
	nonce, err := base64.StdEncoding.DecodeString(r.Nonce)
	if err != nil || len(nonce) != NonceBytes {
		return Snapshot{}, fail(CodeBadInput)
	}
	ct, err := base64.StdEncoding.DecodeString(r.Ciphertext)
	if err != nil {
		return Snapshot{}, fail(CodeBadInput)
	}
	if hexSHA(ct) != r.CiphertextSHA256 {
		return Snapshot{}, fail(CodeHashMismatch)
	}
	secret, ok := master(r.AAD.ContentKeyEpoch)
	if !ok {
		return Snapshot{}, fail(CodeKeyHandoverRequired)
	}
	key, err := DeriveKey(secret)
	if err != nil {
		return Snapshot{}, err
	}
	clear, err := Open(key, r.AAD, nonce, ct)
	if err != nil {
		return Snapshot{}, err
	}
	return DecodeSnapshot(clear)
}

// refusal is Cloud's error envelope (`{error: {code, message, details}}`);
// only the code is kept, never the message.
type refusal struct {
	Error struct {
		Code    string `json:"code"`
		Details struct {
			Persisted string `json:"persisted"`
		} `json:"details"`
	} `json:"error"`
}

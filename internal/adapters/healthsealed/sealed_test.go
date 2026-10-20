package healthsealed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type vector struct {
	MasterSecretHex  string          `json:"master_secret_hex"`
	KeyHex           string          `json:"key_hex"`
	Tenant           string          `json:"tenant"`
	MachineID        string          `json:"machine_id"`
	AAD              json.RawMessage `json:"aad"`
	AADCanonical     string          `json:"aad_canonical"`
	AADSHA256        string          `json:"aad_sha256"`
	NonceHex         string          `json:"nonce_hex"`
	Plaintext        string          `json:"plaintext"`
	CiphertextB64    string          `json:"ciphertext_b64"`
	CiphertextSHA256 string          `json:"ciphertext_sha256"`
	AttachmentID     string          `json:"attachment_id"`
}

func loadVector(t *testing.T) (vector, AAD) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "vector.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var a AAD
	dec := json.NewDecoder(bytes.NewReader(v.AAD))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		t.Fatal(err)
	}
	return v, a
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCommittedVector(t *testing.T) {
	v, aad := loadVector(t)
	key, err := DeriveKey(unhex(t, v.MasterSecretHex))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(key) != v.KeyHex {
		t.Fatalf("K = %x, want %s", key, v.KeyHex)
	}
	canon, err := aad.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(canon) != v.AADCanonical {
		t.Fatalf("canonical AAD\n got %s\nwant %s", canon, v.AADCanonical)
	}
	if d, _ := aad.Digest(); d != v.AADSHA256 {
		t.Fatalf("aad sha %s, want %s", d, v.AADSHA256)
	}
	if got := AttachmentID(v.Tenant, v.MachineID, aad.BootID, aad.SubjectKind, aad.SubjectID); got != v.AttachmentID {
		t.Fatalf("attachment id %s, want %s", got, v.AttachmentID)
	}
	ct, err := Seal(key, aad, unhex(t, v.NonceHex), []byte(v.Plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(ct) != v.CiphertextB64 {
		t.Fatalf("ciphertext %s", base64.StdEncoding.EncodeToString(ct))
	}
	if hexSHA(ct) != v.CiphertextSHA256 {
		t.Fatalf("ciphertext sha %s", hexSHA(ct))
	}
	clear, err := Open(key, aad, unhex(t, v.NonceHex), ct)
	if err != nil || string(clear) != v.Plaintext {
		t.Fatalf("open: %v %q", err, clear)
	}
	snap, err := DecodeSnapshot(clear)
	if err != nil {
		t.Fatalf("the vector plaintext is a valid snapshot: %v", err)
	}
	if snap.Location != "synthetic.go:1" || !snap.AgeNS.Set || snap.AgeNS.Value != 1 {
		t.Fatalf("snapshot %+v", snap)
	}
}

// Every one of the 13 AAD fields is bound: changing any one fails the tag.
func TestEveryAADFieldIsBound(t *testing.T) {
	v, aad := loadVector(t)
	key, _ := DeriveKey(unhex(t, v.MasterSecretHex))
	ct, _ := base64.StdEncoding.DecodeString(v.CiphertextB64)
	nonce := unhex(t, v.NonceHex)
	rv := reflect.ValueOf(&aad).Elem()
	for i := 0; i < rv.NumField(); i++ {
		changed := aad
		f := reflect.ValueOf(&changed).Elem().Field(i)
		name := rv.Type().Field(i).Name
		switch f.Kind() {
		case reflect.String:
			switch name {
			case "Alg", "Schema":
				f.SetString(f.String() + "x")
			case "PreviousCiphertextSHA256":
				f.SetString(strings.Repeat("a", 64))
			default:
				f.SetString(f.String() + "x")
			}
		case reflect.Int64:
			f.SetInt(f.Int() + 1)
		}
		if name != "AttachmentID" && name != "Alg" && name != "Schema" {
			// Keep the derived id consistent so the tag, not validation,
			// is what refuses it.
			changed.AttachmentID = AttachmentID(changed.Tenant, changed.MachineID, changed.BootID, changed.SubjectKind, changed.SubjectID)
		}
		if name == "PreviousCiphertextSHA256" {
			changed.Revision = 2
		}
		if name == "SubjectKind" {
			changed.SubjectKind = SubjectIncident
			changed.AttachmentID = AttachmentID(changed.Tenant, changed.MachineID, changed.BootID, changed.SubjectKind, changed.SubjectID)
		}
		_, err := Open(key, changed, nonce, ct)
		if err == nil {
			t.Fatalf("%s changed and the tag still opened", name)
		}
		if c := CodeOf(err); c == "" {
			t.Fatalf("%s: untyped error %v", name, err)
		}
	}
}

func TestTamperedCiphertextAndDigest(t *testing.T) {
	v, aad := loadVector(t)
	key, _ := DeriveKey(unhex(t, v.MasterSecretHex))
	ct, _ := base64.StdEncoding.DecodeString(v.CiphertextB64)
	bad := append([]byte(nil), ct...)
	bad[3] ^= 1
	if _, err := Open(key, aad, unhex(t, v.NonceHex), bad); CodeOf(err) != CodeTagMismatch {
		t.Fatalf("tampered ciphertext: %v", err)
	}
	other := unhex(t, v.MasterSecretHex)
	other[0] ^= 1
	k2, _ := DeriveKey(other)
	if _, err := Open(k2, aad, unhex(t, v.NonceHex), ct); CodeOf(err) != CodeTagMismatch {
		t.Fatalf("wrong key: %v", err)
	}
	resp := readResponse(t, v, aad, ct)
	resp["ciphertext_sha256"] = strings.Repeat("0", 64)
	if _, err := OpenReadResponse(mustJSON(t, resp), Expect{Tenant: v.Tenant, AttachmentID: v.AttachmentID, Revision: 1}, vectorMaster(t, v)); CodeOf(err) != CodeHashMismatch {
		t.Fatalf("wrong ciphertext digest: %v", err)
	}
}

func vectorMaster(t *testing.T, v vector) MasterFor {
	secret := unhex(t, v.MasterSecretHex)
	return func(epoch int64) ([]byte, bool) {
		if epoch != 1 {
			return nil, false
		}
		return secret, true
	}
}

func readResponse(t *testing.T, v vector, aad AAD, ct []byte) map[string]any {
	t.Helper()
	return map[string]any{
		"schema": ReadSchema, "attachment_id": aad.AttachmentID, "revision": aad.Revision,
		"aad": aad, "aad_sha256": v.AADSHA256, "alg": Alg,
		"nonce":             base64.StdEncoding.EncodeToString(unhex(t, v.NonceHex)),
		"ciphertext":        base64.StdEncoding.EncodeToString(ct),
		"ciphertext_sha256": hexSHA(ct), "received_at_ms": 1, "audit": map[string]any{"recorded": true},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadResponseRefusesSubstitution(t *testing.T) {
	v, aad := loadVector(t)
	ct, _ := base64.StdEncoding.DecodeString(v.CiphertextB64)
	want := Expect{Tenant: v.Tenant, AttachmentID: v.AttachmentID, Revision: 1}
	if s, err := OpenReadResponse(mustJSON(t, readResponse(t, v, aad, ct)), want, vectorMaster(t, v)); err != nil || s.Location != "synthetic.go:1" {
		t.Fatalf("the honest response did not open: %v", err)
	}
	cases := map[string]struct {
		want Expect
		edit func(map[string]any)
		code Code
	}{
		"another account":  {Expect{Tenant: "usr_other", AttachmentID: v.AttachmentID, Revision: 1}, nil, CodeWrongAccount},
		"another revision": {Expect{Tenant: v.Tenant, AttachmentID: v.AttachmentID, Revision: 2}, nil, CodeWrongSubject},
		"another id":       {Expect{Tenant: v.Tenant, AttachmentID: "hs_00000000000000000000000000000000", Revision: 1}, nil, CodeWrongSubject},
		"aad digest lies":  {want, func(m map[string]any) { m["aad_sha256"] = strings.Repeat("1", 64) }, CodeAADMismatch},
		"no audit":         {want, func(m map[string]any) { m["audit"] = map[string]any{"recorded": false} }, CodeRefused},
		"epoch not held": {want, func(m map[string]any) {
			changed := aad
			changed.ContentKeyEpoch = 2
			m["aad"] = changed
			d, _ := changed.Digest()
			m["aad_sha256"] = d
		}, CodeKeyHandoverRequired},
		"unknown field": {want, func(m map[string]any) { m["plaintext"] = "x" }, CodeBadInput},
	}
	for name, c := range cases {
		m := readResponse(t, v, aad, ct)
		if c.edit != nil {
			c.edit(m)
		}
		if _, err := OpenReadResponse(mustJSON(t, m), c.want, vectorMaster(t, v)); CodeOf(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
}

func TestSnapshotKeepsInt64AsStringAndRefusesUnknownFields(t *testing.T) {
	big := int64(1)<<62 + 7
	s := Snapshot{Schema: SnapshotSchema, Location: "x.go:9", AgeNS: Int64{Value: big, Set: true}}
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"age_ns":"4611686018427387911"`) {
		t.Fatalf("int64 not carried as a string: %s", b)
	}
	back, err := DecodeSnapshot(b)
	if err != nil || back.AgeNS.Value != big {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	if !strings.Contains(string(b), `"deadline_ns":null`) {
		t.Fatalf("an unread deadline must be null, not 0: %s", b)
	}
	if _, err := DecodeSnapshot([]byte(`{"schema":"` + SnapshotSchema + `","payload":"secret"}`)); CodeOf(err) != CodeBadInput {
		t.Fatalf("unknown field accepted: %v", err)
	}
	s.Location = strings.Repeat("x", MaxSnapshotFieldChars+1)
	if _, err := s.Encode(); CodeOf(err) != CodeTooLarge {
		t.Fatalf("oversized field: %v", err)
	}
}

// --- uploader ---

type fixedKeys struct {
	mu   sync.Mutex
	keys Keys
	err  error
}

func (f *fixedKeys) Keys(context.Context) (Keys, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys, f.err
}

func testKeys(t *testing.T) *fixedKeys {
	v, _ := loadVector(t)
	return &fixedKeys{keys: Keys{Tenant: v.Tenant, MachineID: v.MachineID, Credential: "machine-cred",
		MasterSecret: unhex(t, v.MasterSecretHex), ContentKeyEpoch: 1, IdentityEpoch: 0}}
}

// fakeCloud is the §13.2 upload and §13.3 read shape, in memory: it derives
// the AAD from the body and the credential's tenant and machine, as Cloud
// does, and refuses what Cloud refuses.
type fakeCloud struct {
	mu       sync.Mutex
	requests atomic.Int64
	identity int64
	revs     map[string][]map[string]any
	force    []int // statuses to answer before behaving
	tenant   string
	machine  string
}

func newFakeCloud(tenant, machine string) *fakeCloud {
	return &fakeCloud{revs: map[string][]map[string]any{}, tenant: tenant, machine: machine}
}

func (c *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.requests.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	refuse := func(status int, code string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"no"}}`))
	}
	if len(c.force) > 0 {
		s := c.force[0]
		c.force = c.force[1:]
		code := map[int]string{503: "health_sealed_deadline", 429: "sealed_quota_exceeded", 410: "sealed_deleted", 409: "identity_epoch_changed", 401: "no_machine_credential"}[s]
		refuse(s, code)
		return
	}
	if r.Header.Get("Authorization") != "Bearer machine-cred" {
		refuse(401, "no_machine_credential")
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var b UploadBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		refuse(400, "unknown_field")
		return
	}
	if b.IdentityEpoch != c.identity {
		refuse(409, "identity_epoch_changed")
		return
	}
	id := AttachmentID(c.tenant, c.machine, b.BootID, b.SubjectKind, b.SubjectID)
	aad := AAD{Alg: b.Alg, AttachmentID: id, BootID: b.BootID, ContentKeyEpoch: b.ContentKeyEpoch,
		ExpiresAtMS: b.ExpiresAtMS, IdentityEpoch: b.IdentityEpoch, MachineID: c.machine,
		PreviousCiphertextSHA256: b.PreviousCiphertextSHA256, Revision: b.Revision, Schema: AADSchema,
		SubjectID: b.SubjectID, SubjectKind: b.SubjectKind, Tenant: c.tenant}
	digest, err := aad.Digest()
	if err != nil {
		refuse(400, "bad_aad")
		return
	}
	list := c.revs[id]
	status := 201
	if int(b.Revision) <= len(list) {
		prior := list[b.Revision-1]
		if prior["body"].(string) != string(raw) {
			refuse(409, "revision_conflict")
			return
		}
		status = 200
	} else if int(b.Revision) != len(list)+1 {
		refuse(409, "revision_out_of_order")
		return
	} else {
		for _, p := range list {
			if p["nonce"] == b.Nonce {
				refuse(409, "nonce_reused")
				return
			}
		}
		c.revs[id] = append(list, map[string]any{"body": string(raw), "nonce": b.Nonce, "aad": aad, "aad_sha256": digest, "b": b})
	}
	st := "stored"
	if status == 200 {
		st = "duplicate"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Receipt{Schema: ReceiptSchema, Status: st, AttachmentID: id, Revision: b.Revision,
		CiphertextSHA256: b.CiphertextSHA256, AADSHA256: digest, ContentKeyEpoch: b.ContentKeyEpoch,
		IdentityEpoch: b.IdentityEpoch, MachineKeyEpoch: 1, ExpiresAtMS: b.ExpiresAtMS})
}

// read is §13.3's response for a stored revision (the proof checks are
// Cloud's; this returns the bytes a proof-checked read returns).
func (c *fakeCloud) read(id string, rev int64) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.revs[id][rev-1]
	b := p["b"].(UploadBody)
	out, _ := json.Marshal(map[string]any{"schema": ReadSchema, "attachment_id": id, "revision": rev,
		"aad": p["aad"], "aad_sha256": p["aad_sha256"], "alg": Alg, "nonce": b.Nonce, "ciphertext": b.Ciphertext,
		"ciphertext_sha256": b.CiphertextSHA256, "received_at_ms": 1, "audit": map[string]any{"recorded": true}})
	return out
}

func newUploader(t *testing.T, srv *httptest.Server, keys KeySource, enabled *atomic.Bool) *Uploader {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "sealed"))
	if err != nil {
		t.Fatal(err)
	}
	return &Uploader{Endpoint: srv.URL, Client: srv.Client(), Enabled: enabled.Load, Keys: keys, Store: store,
		Now:   func() time.Time { return time.UnixMilli(1787745600000) },
		Sleep: func(context.Context, time.Duration) error { return nil }}
}

func snap(loc string) Snapshot {
	return Snapshot{Schema: SnapshotSchema, Location: loc, Stage: "relay_write", Build: "dev-test",
		AgeNS: Int64{Value: 1 << 60, Set: true}, Coverage: []Coverage{{Source: "journal", Lost: Int64{Value: 0, Set: true}}}}
}

func TestDisabledMakesNoRequest(t *testing.T) {
	v, _ := loadVector(t)
	cloud := newFakeCloud(v.Tenant, v.MachineID)
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool // default off
	u := newUploader(t, srv, testKeys(t), &on)
	if _, err := u.Seal(context.Background(), "boot-1", SubjectEvidence, "ev-1", snap("a.go:1")); CodeOf(err) != CodeDisabled {
		t.Fatalf("seal while off: %v", err)
	}
	w := NewWorker(u)
	if err := w.Offer(Job{BootID: "boot-1", SubjectKind: SubjectEvidence, SubjectID: "ev-1", Snapshot: snap("a")}); CodeOf(err) != CodeDisabled {
		t.Fatalf("offer while off: %v", err)
	}
	if n := cloud.requests.Load(); n != 0 {
		t.Fatalf("%d requests while off", n)
	}
	ids, _ := u.Store.IDs()
	if len(ids) != 0 {
		t.Fatalf("records written while off: %v", ids)
	}
}

func TestMissingContentEpochIsHandoverRequired(t *testing.T) {
	src := ProviderSource{
		Identity: func(context.Context) (string, string, string, int64, error) { return "usr", "mac", "cred", 0, nil },
		Master:   func() ([]byte, bool, error) { return make([]byte, 32), true, nil },
	}
	if _, err := src.Keys(context.Background()); CodeOf(err) != CodeKeyHandoverRequired {
		t.Fatalf("no content epoch: %v", err)
	}
	src.ContentEpoch = func() (int64, bool) { return 1, true }
	src.Master = func() ([]byte, bool, error) { return nil, false, nil }
	if _, err := src.Keys(context.Background()); CodeOf(err) != CodeKeyUnavailable {
		t.Fatalf("no master secret: %v", err)
	}
}

// The real client code uploads, Cloud derives the AAD, a read response is
// opened with the held master secret.
func TestUploadThenReadThenOpen(t *testing.T) {
	v, _ := loadVector(t)
	cloud := newFakeCloud(v.Tenant, v.MachineID)
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	ctx := context.Background()
	for i, loc := range []string{"first.go:1", "second.go:2"} {
		p, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap(loc))
		if err != nil {
			t.Fatal(err)
		}
		if p.Revision != int64(i+1) {
			t.Fatalf("revision %d", p.Revision)
		}
		rc, err := u.Send(ctx, p.AttachmentID)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Status != "stored" {
			t.Fatalf("receipt %+v", rc)
		}
		got, err := OpenReadResponse(cloud.read(p.AttachmentID, p.Revision),
			Expect{Tenant: v.Tenant, AttachmentID: p.AttachmentID, Revision: p.Revision}, vectorMaster(t, v))
		if err != nil {
			t.Fatal(err)
		}
		if got.Location != loc || got.AgeNS.Value != 1<<60 {
			t.Fatalf("opened %+v", got)
		}
	}
	// Revision 1 under revision 2's request: the chain digest is bound.
	a, _, _ := u.Store.Load(AttachmentID(v.Tenant, v.MachineID, "boot-1", SubjectEvidence, "ev-1"))
	if a.Revisions[0].Nonce == a.Revisions[1].Nonce {
		t.Fatal("a nonce was reused within an attachment")
	}
}

// A crash after the body is written and before Cloud answers: the restarted
// uploader sends identical bytes, and Cloud answers duplicate.
func TestRestartResendsIdenticalBody(t *testing.T) {
	v, _ := loadVector(t)
	cloud := newFakeCloud(v.Tenant, v.MachineID)
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	ctx := context.Background()
	p, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("x.go:1"))
	if err != nil {
		t.Fatal(err)
	}
	before, _, _ := u.Store.Load(p.AttachmentID)
	if _, err := u.Send(ctx, p.AttachmentID); err != nil {
		t.Fatal(err)
	}
	// Simulate "sent but the receipt was lost": put the record back as it
	// was on disk before the answer, then restart on the same directory.
	if err := u.Store.Put(before); err != nil {
		t.Fatal(err)
	}
	store2, _ := OpenStore(u.Store.dir)
	u2 := *u
	u2.Store = store2
	if _, err := u2.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("y.go:2")); CodeOf(err) != CodeBusy {
		t.Fatalf("a new revision was sealed over a pending one: %v", err)
	}
	rc, err := u2.Send(ctx, p.AttachmentID)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Status != "duplicate" {
		t.Fatalf("resend was not byte-identical: %+v", rc)
	}
}

func TestRetryThenTypedRefusals(t *testing.T) {
	v, _ := loadVector(t)
	ctx := context.Background()
	cases := []struct {
		force []int
		code  Code
		state RevisionState
	}{
		{[]int{503, 503}, "", RevisionStored},                      // deadline: same body retried
		{[]int{503, 503, 503, 503}, CodeUpstream, RevisionPending}, // attempts exhausted, kept
		{[]int{409}, CodeIdentityChanged, RevisionRefused},         // identity epoch changed
		{[]int{410}, CodeDeleted, RevisionRefused},                 // deleted
		{[]int{429}, CodeQuota, RevisionPending},                   // quota: not retried now
		{[]int{401}, CodeAuthRefused, RevisionPending},             // credential refused
	}
	for _, c := range cases {
		cloud := newFakeCloud(v.Tenant, v.MachineID)
		cloud.force = c.force
		srv := httptest.NewServer(cloud)
		var on atomic.Bool
		on.Store(true)
		u := newUploader(t, srv, testKeys(t), &on)
		p, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("x"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = u.Send(ctx, p.AttachmentID)
		if CodeOf(err) != c.code {
			t.Errorf("%v: %v, want %q", c.force, err, c.code)
		}
		if n := cloud.requests.Load(); n > MaxAttempts {
			t.Errorf("%v: %d requests, bound %d", c.force, n, MaxAttempts)
		}
		a, _, _ := u.Store.Load(p.AttachmentID)
		if a.Revisions[0].State != c.state {
			t.Errorf("%v: state %s, want %s", c.force, a.Revisions[0].State, c.state)
		}
		srv.Close()
	}
}

func TestReceiptMustNameOwnAAD(t *testing.T) {
	v, _ := loadVector(t)
	// Cloud derives the AAD from another machine id: the receipt's
	// aad_sha256 differs and the upload is failed, never marked stored.
	cloud := newFakeCloud(v.Tenant, "mac_other")
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	p, _ := u.Seal(context.Background(), "boot-1", SubjectEvidence, "ev-1", snap("x"))
	if _, err := u.Send(context.Background(), p.AttachmentID); CodeOf(err) != CodeReceiptMismatch {
		t.Fatalf("mismatched receipt: %v", err)
	}
}

func TestWrongEpochAccountMachineAndNonceReuse(t *testing.T) {
	v, _ := loadVector(t)
	cloud := newFakeCloud(v.Tenant, v.MachineID)
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	keys := testKeys(t)
	u := newUploader(t, srv, keys, &on)
	// A fixed nonce source: the second revision would reuse the first nonce.
	u.Rand = bytes.NewReader(bytes.Repeat([]byte{7}, 64))
	ctx := context.Background()
	p, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Send(ctx, p.AttachmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("y")); CodeOf(err) != CodeNonceReused {
		t.Fatalf("nonce reuse: %v", err)
	}
	keys.mu.Lock()
	keys.keys.ContentKeyEpoch = 2
	keys.mu.Unlock()
	if _, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("y")); CodeOf(err) != CodeWrongEpoch {
		t.Fatalf("another epoch's key on the same attachment: %v", err)
	}
	keys.mu.Lock()
	keys.keys.ContentKeyEpoch = 1
	keys.keys.IdentityEpoch = 3
	keys.mu.Unlock()
	if _, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("y")); CodeOf(err) != CodeIdentityChanged {
		t.Fatalf("identity epoch moved: %v", err)
	}
	// A record on disk for another machine under this id is never extended.
	a, _, _ := u.Store.Load(p.AttachmentID)
	a.MachineID = "mac_else"
	_ = u.Store.Put(a)
	keys.mu.Lock()
	keys.keys.IdentityEpoch = 0
	keys.mu.Unlock()
	if _, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-1", snap("y")); CodeOf(err) != CodeWrongMachine {
		t.Fatalf("another machine's record: %v", err)
	}
}

func TestFullStoreAndQueueRefuseAtOnce(t *testing.T) {
	v, _ := loadVector(t)
	cloud := newFakeCloud(v.Tenant, v.MachineID)
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	ctx := context.Background()
	for i := 0; i < MaxPendingAttachments; i++ {
		if _, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-"+string(rune('a'+i)), snap("x")); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	if _, err := u.Seal(ctx, "boot-1", SubjectEvidence, "ev-full", snap("x")); CodeOf(err) != CodeStoreFull {
		t.Fatalf("full store: %v", err)
	}
	// The worker is not running: the queue fills and Offer refuses without
	// waiting for it.
	w := NewWorker(u)
	var busy int
	for i := 0; i < MaxQueuedJobs+3; i++ {
		if err := w.Offer(Job{BootID: "boot-1", SubjectKind: SubjectEvidence, SubjectID: "q", Snapshot: snap("x")}); CodeOf(err) == CodeBusy {
			busy++
		}
	}
	if busy != 3 || w.Status().Dropped != 3 {
		t.Fatalf("busy %d dropped %d", busy, w.Status().Dropped)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("refusing took %s", d)
	}
	if n := cloud.requests.Load(); n != 0 {
		t.Fatalf("%d requests", n)
	}
}

func TestWorkerRunsJobs(t *testing.T) {
	v, _ := loadVector(t)
	cloud := newFakeCloud(v.Tenant, v.MachineID)
	srv := httptest.NewServer(cloud)
	defer srv.Close()
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	w := NewWorker(u)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	if err := w.Offer(Job{BootID: "boot-1", SubjectKind: SubjectEvidence, SubjectID: "ev-w", Snapshot: snap("w.go:1")}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for w.Status().Stored != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("status %+v", w.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestOptInIsOnlyAJSONTrue(t *testing.T) {
	cases := map[string]bool{`true`: true, `false`: false, `"true"`: false, `1`: false, `null`: false}
	for raw, want := range cases {
		if got := OptedIn(map[string]json.RawMessage{SettingKey: json.RawMessage(raw)}); got != want {
			t.Errorf("%s: %v", raw, got)
		}
	}
	if OptedIn(map[string]json.RawMessage{"cloud_health_upload": json.RawMessage(`true`)}) {
		t.Fatal("the metadata upload switch alone enabled owner retention")
	}
	failing := EnabledFrom(func() (map[string]json.RawMessage, error) { return nil, os.ErrPermission })
	if failing() {
		t.Fatal("an unreadable settings file enabled owner retention")
	}
	if EnabledFrom(nil)() {
		t.Fatal("no settings reader enabled owner retention")
	}
}

package healthsealed

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// UploadPath is §13.2's machine route.
const UploadPath = "/v1/health/sealed/attachments"

// Bounds, registered in internal/domain/capacity.
const (
	// MaxAttempts bounds one revision's sends in one Send call; past it the
	// revision stays pending with its exact body for the next call.
	MaxAttempts = 4
	// MaxAttemptSeconds is one request's whole deadline.
	MaxAttemptSeconds = 10
	// MaxResponseBytes is how much of an answer is read.
	MaxResponseBytes = 16 << 10
	// MaxQueuedJobs is the worker's hand-off queue. At the bound Offer
	// refuses at once (CodeBusy); it never waits.
	MaxQueuedJobs = 8
)

const (
	firstBackoff   = time.Second
	backoffCeiling = 8 * time.Second
)

// SettingKey is the owner-diagnostic-retention opt-in in the app's settings
// file (internal/adapters/nextconfig). It is separate from
// cloud_health_upload: the metadata switch alone never permits this.
const SettingKey = "cloud_health_owner_retention"

// OptedIn is true only for a JSON true under SettingKey; absent, null,
// anything else, or a settings file that could not be read is off.
func OptedIn(values map[string]json.RawMessage) bool {
	raw, ok := values[SettingKey]
	if !ok {
		return false
	}
	var on bool
	return json.Unmarshal(raw, &on) == nil && on
}

// EnabledFrom builds Uploader.Enabled from a settings reader, read on every
// call so turning the setting off stops the next seal and send.
func EnabledFrom(read func() (map[string]json.RawMessage, error)) func() bool {
	return func() bool {
		if read == nil {
			return false
		}
		values, err := read()
		return err == nil && OptedIn(values)
	}
}

// Keys is everything one seal needs, read fresh for each seal and send.
type Keys struct {
	Tenant          string
	MachineID       string
	Credential      string
	MasterSecret    []byte
	ContentKeyEpoch int64
	IdentityEpoch   int64
}

// KeySource reads the machine's identity and master secret. It returns a
// typed Error: CodeKeyHandoverRequired when the content-key epoch of the
// master secret is not known, CodeKeyUnavailable when no secret is held.
// Each call answers a MasterSecret of its own: the caller clears it as soon
// as the key is derived, so it never outlives one seal.
type KeySource interface {
	Keys(ctx context.Context) (Keys, error)
}

// ProviderSource is the production KeySource shape: each part comes from
// the place that already owns it (the device token for tenant, machine and
// credential; cloudkeys for the master secret). ContentEpoch is required:
// until the pairing handover records which content-key epoch the master
// secret is, it is nil and every seal is CodeKeyHandoverRequired.
type ProviderSource struct {
	Identity     func(ctx context.Context) (tenant, machine, credential string, identityEpoch int64, err error)
	Master       func() ([]byte, bool, error)
	ContentEpoch func() (int64, bool)
}

func (p ProviderSource) Keys(ctx context.Context) (Keys, error) {
	if p.ContentEpoch == nil {
		return Keys{}, fail(CodeKeyHandoverRequired)
	}
	epoch, ok := p.ContentEpoch()
	if !ok || epoch < 1 {
		return Keys{}, fail(CodeKeyHandoverRequired)
	}
	if p.Identity == nil || p.Master == nil {
		return Keys{}, fail(CodeKeyUnavailable)
	}
	tenant, machine, cred, identityEpoch, err := p.Identity(ctx)
	if err != nil || tenant == "" || machine == "" || cred == "" {
		return Keys{}, fail(CodeKeyUnavailable)
	}
	secret, ok, err := p.Master()
	if err != nil || !ok || len(secret) != 32 {
		return Keys{}, fail(CodeKeyUnavailable)
	}
	return Keys{Tenant: tenant, MachineID: machine, Credential: cred, MasterSecret: secret,
		ContentKeyEpoch: epoch, IdentityEpoch: identityEpoch}, nil
}

// Uploader seals snapshots into the store and sends stored revisions.
type Uploader struct {
	// Endpoint is Cloud's API origin, https only (http for loopback tests).
	Endpoint string
	// Origin, when set, is read instead of Endpoint for every send, so the
	// origin the settings name now is the one used.
	Origin func() (string, error)
	// Ready, when set, is a local-only check (no request) of whether a seal
	// could happen now; Worker.Refresh reports its code, so a missing key
	// handover is visible before the first incident needs it.
	Ready  func() error
	Client *http.Client
	// Enabled is the owner-diagnostic-retention opt-in. It is separate from
	// the metadata upload switch and off by default; off means no seal and
	// no request.
	Enabled func() bool
	Keys    KeySource
	Store   *Store
	Now     func() time.Time
	Rand    io.Reader
	Sleep   func(ctx context.Context, d time.Duration) error
}

func (u *Uploader) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Uploader) enabled() bool { return u.Enabled != nil && u.Enabled() }

// Pending names one sealed revision waiting to be sent.
type Pending struct {
	AttachmentID string
	Revision     int64
}

// Seal encrypts snap as the next revision of its subject's attachment and
// writes the exact request body to the store before anything is sent.
func (u *Uploader) Seal(ctx context.Context, bootID, kind, subjectID string, snap Snapshot) (Pending, error) {
	if !u.enabled() {
		return Pending{}, fail(CodeDisabled)
	}
	if u.Keys == nil || u.Store == nil {
		return Pending{}, fail(CodeKeyUnavailable)
	}
	keys, err := u.Keys.Keys(ctx)
	if err != nil {
		return Pending{}, err
	}
	defer clear(keys.MasterSecret)
	plain, err := snap.Encode()
	if err != nil {
		return Pending{}, err
	}
	id := AttachmentID(keys.Tenant, keys.MachineID, bootID, kind, subjectID)
	nowMS := u.now().UnixMilli()
	a, exists, err := u.Store.Load(id)
	if err != nil {
		return Pending{}, err
	}
	if exists {
		switch {
		case a.Tenant != keys.Tenant:
			return Pending{}, fail(CodeWrongAccount)
		case a.MachineID != keys.MachineID:
			return Pending{}, fail(CodeWrongMachine)
		case a.ContentKeyEpoch != keys.ContentKeyEpoch:
			// One attachment is sealed under one key; a different epoch's
			// key is never used for it.
			return Pending{}, fail(CodeWrongEpoch)
		case a.IdentityEpoch != keys.IdentityEpoch:
			return Pending{}, fail(CodeIdentityChanged)
		case a.ExpiresAtMS < nowMS+minLifeMS:
			return Pending{}, fail(CodeExpired)
		case len(a.Revisions) >= MaxRevisions:
			return Pending{}, fail(CodeRevisionsExhausted)
		}
		last := a.Revisions[len(a.Revisions)-1]
		if last.State == RevisionPending {
			return Pending{}, fail(CodeBusy)
		}
		if last.State == RevisionRefused {
			return Pending{}, &Error{Code: CodeRefused, Cloud: last.Refusal}
		}
	} else {
		a = Attachment{ID: id, Tenant: keys.Tenant, MachineID: keys.MachineID, BootID: bootID,
			SubjectKind: kind, SubjectID: subjectID, ContentKeyEpoch: keys.ContentKeyEpoch,
			IdentityEpoch: keys.IdentityEpoch, OpenedAtMS: nowMS, ExpiresAtMS: nowMS + MaxLifeSeconds*1000}
	}
	prev := ""
	if n := len(a.Revisions); n > 0 {
		prev = a.Revisions[n-1].CiphertextSHA256
	}
	aad := AAD{Alg: Alg, AttachmentID: id, BootID: bootID, ContentKeyEpoch: keys.ContentKeyEpoch,
		ExpiresAtMS: a.ExpiresAtMS, IdentityEpoch: keys.IdentityEpoch, MachineID: keys.MachineID,
		PreviousCiphertextSHA256: prev, Revision: int64(len(a.Revisions) + 1), Schema: AADSchema,
		SubjectID: subjectID, SubjectKind: kind, Tenant: keys.Tenant}
	aadDigest, err := aad.Digest()
	if err != nil {
		return Pending{}, err
	}
	nonce := make([]byte, NonceBytes)
	src := u.Rand
	if src == nil {
		src = rand.Reader
	}
	if _, err := io.ReadFull(src, nonce); err != nil {
		return Pending{}, fail(CodeKeyUnavailable)
	}
	nonceText := base64.StdEncoding.EncodeToString(nonce)
	for _, r := range a.Revisions {
		if r.Nonce == nonceText {
			return Pending{}, fail(CodeNonceReused)
		}
	}
	key, err := DeriveKey(keys.MasterSecret)
	clear(keys.MasterSecret)
	if err != nil {
		return Pending{}, err
	}
	ct, err := Seal(key, aad, nonce, plain)
	clear(key)
	if err != nil {
		return Pending{}, err
	}
	ctDigest := hexSHA(ct)
	body, err := json.Marshal(UploadBody{Schema: AttachmentSchema, Alg: Alg, BootID: bootID,
		SubjectKind: kind, SubjectID: subjectID, Revision: aad.Revision,
		PreviousCiphertextSHA256: prev, ContentKeyEpoch: keys.ContentKeyEpoch,
		IdentityEpoch: keys.IdentityEpoch, ExpiresAtMS: a.ExpiresAtMS, Nonce: nonceText,
		Ciphertext: base64.StdEncoding.EncodeToString(ct), CiphertextSHA256: ctDigest})
	if err != nil {
		return Pending{}, fail(CodeBadInput)
	}
	if len(body) > MaxRequestBytes {
		return Pending{}, fail(CodeTooLarge)
	}
	a.Revisions = append(a.Revisions, Revision{Revision: aad.Revision, Nonce: nonceText,
		CiphertextSHA256: ctDigest, AADSHA256: aadDigest, Body: body, State: RevisionPending})
	if err := u.Store.Put(a); err != nil {
		return Pending{}, err
	}
	return Pending{AttachmentID: id, Revision: aad.Revision}, nil
}

// Send sends the attachment's pending revision, exactly as stored, until a
// receipt, a refusal or MaxAttempts. A revision that ran out of attempts
// stays pending with the same bytes.
func (u *Uploader) Send(ctx context.Context, id string) (Receipt, error) {
	if !u.enabled() {
		return Receipt{}, fail(CodeDisabled)
	}
	keys, err := u.Keys.Keys(ctx)
	if err != nil {
		return Receipt{}, err
	}
	// A send needs the identity, never the secret.
	clear(keys.MasterSecret)
	a, ok, err := u.Store.Load(id)
	if err != nil {
		return Receipt{}, err
	}
	if !ok {
		return Receipt{}, fail(CodeBadInput)
	}
	idx := -1
	for i, r := range a.Revisions {
		if r.State == RevisionPending {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Receipt{}, fail(CodeBadInput)
	}
	if a.Tenant != keys.Tenant || a.MachineID != keys.MachineID {
		return Receipt{}, fail(CodeWrongAccount)
	}
	if a.IdentityEpoch != keys.IdentityEpoch {
		return Receipt{}, u.refuse(a, idx, &Error{Code: CodeIdentityChanged})
	}
	rev := a.Revisions[idx]
	target, err := u.target()
	if err != nil {
		return Receipt{}, err
	}
	backoff := firstBackoff
	var last error
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		receipt, retry, err := u.post(ctx, target, keys.Credential, rev.Body)
		if err == nil {
			if err := checkReceipt(receipt, a, rev); err != nil {
				return Receipt{}, u.refuse(a, idx, err)
			}
			a.Revisions[idx].State = RevisionStored
			if err := u.Store.Put(a); err != nil {
				return receipt, err
			}
			return receipt, nil
		}
		last = err
		if !retry {
			if permanent(err) {
				return Receipt{}, u.refuse(a, idx, err)
			}
			return Receipt{}, err
		}
		if attempt == MaxAttempts {
			break
		}
		if err := u.sleep(ctx, backoff); err != nil {
			return Receipt{}, fail(CodeUpstream)
		}
		backoff = min(backoff*2, backoffCeiling)
	}
	return Receipt{}, last
}

func (u *Uploader) refuse(a Attachment, idx int, err error) error {
	var e *Error
	cloud := ""
	if asError(err, &e) {
		cloud = e.Cloud
		if cloud == "" {
			cloud = string(e.Code)
		}
	}
	a.Revisions[idx].State = RevisionRefused
	a.Revisions[idx].Refusal = cloud
	if perr := u.Store.Put(a); perr != nil {
		return perr
	}
	return err
}

func (u *Uploader) sleep(ctx context.Context, d time.Duration) error {
	if u.Sleep != nil {
		return u.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (u *Uploader) target() (string, error) {
	endpoint := u.Endpoint
	if u.Origin != nil {
		e, err := u.Origin()
		if err != nil {
			return "", fail(CodeKeyUnavailable)
		}
		endpoint = e
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "", fail(CodeBadInput)
	}
	host := parsed.Hostname()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (host == "127.0.0.1" || host == "localhost" || host == "::1")) {
		return "", fail(CodeBadInput)
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/") + UploadPath, nil
}

func (u *Uploader) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	return &http.Client{Timeout: MaxAttemptSeconds * time.Second}
}

// post makes one request. retry says the same body may be sent again.
func (u *Uploader) post(ctx context.Context, target, credential string, body []byte) (Receipt, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, MaxAttemptSeconds*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, false, fail(CodeBadInput)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := u.client().Do(req)
	if err != nil {
		return Receipt{}, true, fail(CodeUpstream)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(raw) > MaxResponseBytes {
		return Receipt{}, true, fail(CodeUpstream)
	}
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		var r Receipt
		if err := json.Unmarshal(raw, &r); err != nil {
			return Receipt{}, false, fail(CodeReceiptMismatch)
		}
		return r, false, nil
	}
	var ref refusal
	_ = json.Unmarshal(raw, &ref)
	code := ref.Error.Code
	e := &Error{Code: CodeRefused, Cloud: code}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		e.Code = CodeAuthRefused
		return Receipt{}, false, e
	case code == "identity_epoch_changed":
		e.Code = CodeIdentityChanged
	case code == "content_key_epoch_regressed":
		e.Code = CodeEpochRegressed
	case code == "revision_conflict" || code == "revision_out_of_order" || code == "previous_digest_mismatch":
		e.Code = CodeRevisionConflict
	case code == "nonce_reused":
		e.Code = CodeNonceReused
	case code == "sealed_deleted":
		e.Code = CodeDeleted
	case code == "sealed_expired":
		e.Code = CodeExpired
	case code == "sealed_quota_exceeded":
		e.Code = CodeQuota
	case code == "health_sealed_busy" || code == "health_sealed_deadline" ||
		resp.StatusCode >= 500 && code != "health_sealed_disabled" && code != "health_sealed_unavailable":
		e.Code = CodeUpstream
		return Receipt{}, true, e
	case code == "health_sealed_disabled" || code == "health_sealed_unavailable":
		e.Code = CodeUpstream
	}
	return Receipt{}, false, e
}

// permanent is a refusal after which this revision's bytes can never be
// accepted; anything else leaves it pending.
func permanent(err error) bool {
	switch CodeOf(err) {
	case CodeIdentityChanged, CodeEpochRegressed, CodeRevisionConflict, CodeNonceReused,
		CodeDeleted, CodeExpired, CodeRefused, CodeReceiptMismatch:
		return true
	}
	return false
}

// checkReceipt is §13.2's "compare aad_sha256 with its own AAD digest", and
// every other field the receipt repeats.
func checkReceipt(r Receipt, a Attachment, rev Revision) error {
	if r.Schema != ReceiptSchema || (r.Status != "stored" && r.Status != "duplicate") {
		return fail(CodeReceiptMismatch)
	}
	if r.AttachmentID != a.ID || r.Revision != rev.Revision || r.CiphertextSHA256 != rev.CiphertextSHA256 ||
		r.AADSHA256 != rev.AADSHA256 || r.ContentKeyEpoch != a.ContentKeyEpoch || r.IdentityEpoch != a.IdentityEpoch {
		return fail(CodeReceiptMismatch)
	}
	return nil
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

// Job is one snapshot handed to the worker.
type Job struct {
	BootID      string
	SubjectKind string
	SubjectID   string
	Snapshot    Snapshot
}

// Status is the worker's last outcome, for the local status route.
type Status struct {
	Enabled bool `json:"enabled"`
	// Ready is Uploader.Ready's code at the last refresh; empty when a
	// seal could happen.
	Ready    Code   `json:"ready_code,omitempty"`
	Code     Code   `json:"code,omitempty"`
	Cloud    string `json:"cloud_code,omitempty"`
	Stored   int64  `json:"stored"`
	Refused  int64  `json:"refused"`
	Dropped  int64  `json:"dropped"`
	LastAtMS int64  `json:"last_at_ms,omitempty"`
	// Queued is the hand-off queue's depth now (MaxQueuedJobs).
	Queued int `json:"queued"`
	// Attachments and StoreBytes are the local records as last counted on
	// the worker's goroutine; StoreMeasured is false until then, and
	// after a count that failed.
	Attachments   int   `json:"attachments"`
	StoreBytes    int64 `json:"store_bytes"`
	StoreMeasured bool  `json:"store_measured"`
}

// Worker runs seals and sends on its own goroutine, so a producer — the
// watchdog, the journal — only ever does a non-blocking Offer.
type Worker struct {
	u    *Uploader
	jobs chan Job
	// Interval is how often Run re-reads the opt-in, re-checks readiness
	// and resends revisions left pending (by a restart, a shutdown or
	// spent attempts).
	Interval time.Duration
	// on caches the opt-in as last read on the worker's goroutine, so the
	// status route and Offer do no file I/O while it is on.
	on atomic.Bool
	mu sync.Mutex
	st Status
}

// refreshInterval is Worker.Interval's default.
const refreshInterval = time.Minute

// NewWorker binds an uploader to a bounded hand-off queue.
func NewWorker(u *Uploader) *Worker {
	return &Worker{u: u, jobs: make(chan Job, MaxQueuedJobs), Interval: refreshInterval}
}

// Enabled is the opt-in as last read; it never reads a file.
func (w *Worker) Enabled() bool { return w.on.Load() }

// Offer hands a job to the worker without waiting. Off is CodeDisabled; a
// full queue is CodeBusy and the job is counted as dropped. While the
// cached opt-in is on it does no I/O; the seal and the send re-read it.
func (w *Worker) Offer(j Job) error {
	if !w.on.Load() {
		if !w.u.enabled() {
			return fail(CodeDisabled)
		}
		w.on.Store(true)
	}
	select {
	case w.jobs <- j:
		return nil
	default:
		w.mu.Lock()
		w.st.Dropped++
		w.mu.Unlock()
		return fail(CodeBusy)
	}
}

// Run serves jobs until ctx ends. It first resends what an earlier run
// left pending, then does so again every Interval. A job in flight when
// ctx ends stays pending on disk with its exact body for the next run.
func (w *Worker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = refreshInterval
	}
	w.Refresh()
	w.resume(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Refresh()
			w.resume(ctx)
		case j := <-w.jobs:
			w.handle(ctx, j)
		}
	}
}

// Refresh re-reads the opt-in and, when on, the uploader's local readiness.
func (w *Worker) Refresh() {
	on := w.u.enabled()
	w.on.Store(on)
	var ready Code
	if on && w.u.Ready != nil {
		if err := w.u.Ready(); err != nil {
			if ready = CodeOf(err); ready == "" {
				ready = CodeKeyUnavailable
			}
		}
	}
	w.mu.Lock()
	w.st.Ready = ready
	w.mu.Unlock()
	w.measure()
}

// measure counts the local records for the status; it runs only on the
// worker's goroutine, so the status route never reads the disk.
func (w *Worker) measure() {
	if w.u.Store == nil {
		return
	}
	n, total, err := w.u.Store.Usage()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.st.Attachments, w.st.StoreBytes, w.st.StoreMeasured = n, total, err == nil
}

// resume prunes expired records and sends every revision still pending,
// at most MaxPendingAttachments of them, each within its own bounds.
func (w *Worker) resume(ctx context.Context) {
	if !w.on.Load() || w.u.Store == nil {
		return
	}
	_ = w.u.Store.Prune(w.u.now().UnixMilli())
	ids, err := w.u.Store.IDs()
	if err != nil {
		w.record(err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		a, ok, err := w.u.Store.Load(id)
		if err != nil || !ok || len(a.Revisions) == 0 || a.Revisions[len(a.Revisions)-1].State != RevisionPending {
			continue
		}
		_, err = w.u.Send(ctx, id)
		if ctx.Err() != nil {
			return
		}
		w.record(err)
	}
}

func (w *Worker) handle(ctx context.Context, j Job) {
	p, err := w.u.Seal(ctx, j.BootID, j.SubjectKind, j.SubjectID, j.Snapshot)
	if err == nil {
		_, err = w.u.Send(ctx, p.AttachmentID)
	}
	if ctx.Err() != nil {
		// Stopping: the revision, if sealed, is pending on disk.
		return
	}
	w.record(err)
	w.measure()
}

func (w *Worker) record(err error) {
	if CodeOf(err) == CodeDisabled {
		w.on.Store(false)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.st.LastAtMS = w.u.now().UnixMilli()
	w.st.Code, w.st.Cloud = "", ""
	if err == nil {
		w.st.Stored++
		return
	}
	var e *Error
	if asError(err, &e) {
		w.st.Code, w.st.Cloud = e.Code, e.Cloud
	} else {
		w.st.Code = CodeUpstream
	}
	if permanent(err) {
		w.st.Refused++
	}
}

// Status is a snapshot; it never waits on the worker.
func (w *Worker) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.st
	s.Enabled = w.on.Load()
	s.Queued = len(w.jobs)
	return s
}

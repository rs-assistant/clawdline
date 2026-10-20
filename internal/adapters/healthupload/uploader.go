// Package healthupload sends the durable connection-health store's records to
// Cloud's machine-authenticated ingest (clawdline-cloud
// docs/connection-health-contract.md §2) over its own HTTPS client, so the
// evidence of a stuck Cloud line can leave the machine while that line — its
// relay socket, its writer lock, its spool — is the thing that is stuck.
//
// It is off unless the person opted in, Cloud is on and the machine has an
// identity; off means no request at all. It sends only content-free
// metadata (wire.go); owner diagnostics stay on this machine until owner
// sealing exists. One batch is in flight at a time, its exact body is kept
// on disk and resent under the same batch_id until Cloud answers with a
// durable receipt or the batch's bounded attempts and time run out, and
// every request counts against a per-machine hourly budget below Cloud's.
// Nothing here is awaited by business code: Status reads a snapshot, and the
// one goroutine is Run's.
package healthupload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// Bounds. Each is registered in internal/domain/capacity
// (register_healthupload.go).
const (
	// MaxBatchBytes is Cloud's body limit (413 above it).
	MaxBatchBytes = 32 << 10
	// MaxBatchEvents is Cloud's records-per-batch limit.
	MaxBatchEvents = 100
	// MaxHourlyBytes is the machine's whole hourly allowance; Cloud holds
	// MetadataReserveBytes of it back, so routine uploads stop at
	// MaxHourlyRoutineBytes.
	MaxHourlyBytes        = 1 << 20
	MetadataReserveBytes  = 64 << 10
	MaxHourlyRoutineBytes = MaxHourlyBytes - MetadataReserveBytes
	// MaxHourlyRequests is Cloud's per-machine request budget per clock
	// hour.
	MaxHourlyRequests = 120
	// MaxAttempts and MaxRetrySeconds bound one batch: past either it is
	// abandoned and counted, never retried forever.
	MaxAttempts     = 6
	MaxRetrySeconds = 60 * 60
	// MaxRetryAfterSeconds caps a server's Retry-After.
	MaxRetryAfterSeconds = 60 * 60
	// MaxAttemptSeconds is one request's whole deadline: dial, TLS, send,
	// answer.
	MaxAttemptSeconds = 10
	// MaxResponseBytes is how much of an answer is read.
	MaxResponseBytes = 16 << 10
	// MaxStateBytes bounds the progress file.
	MaxStateBytes = 64 << 10
	// MaxReceipts is how many receipts the progress file keeps.
	MaxReceipts = 16
)

// Cadences (not held state).
const (
	idleInterval   = 30 * time.Second
	firstBackoff   = 5 * time.Second
	maxBackoff     = 5 * time.Minute
	stateRetry     = time.Minute
	headroomBytes  = 512
	pageLimit      = connectionhealth.MaxQueryRecords
	maxBuildRounds = 8
)

// Code is a stable state or failure a caller can branch on.
type Code string

const (
	// Off states: no request is made in any of them.
	CodeNotOptedIn      Code = "not_opted_in"
	CodeCloudOff        Code = "cloud_off"
	CodeNoIdentity      Code = "no_identity"
	CodeIdentityUnread  Code = "identity_unreadable"
	CodeOtherOrigin     Code = "origin_refused"
	CodeSourceOpening   Code = "source_not_open"
	CodeSourceFailed    Code = "source_unavailable"
	CodeStateUnwritable Code = "state_unwritable"
	CodeStateUnreadable Code = "state_unreadable"
	// Working states.
	CodeIdle       Code = "idle"
	CodeUploading  Code = "uploading"
	CodeBackoff    Code = "backoff"
	CodeQuotaLocal Code = "quota_local"
	CodeQuotaCloud Code = "quota_cloud"
	// CodeAuthRefused: Cloud refused the credential (401/403). No further
	// request is made with that credential.
	CodeAuthRefused Code = "auth_refused"
	// CodeUpstreamUnavailable: network failure, timeout or 5xx.
	CodeUpstreamUnavailable Code = "upstream_unavailable"
	// CodeRefused: Cloud refused a batch for good; the batch is counted and
	// the next one is built.
	CodeRefused Code = "batch_refused"
	CodeStopped Code = "stopped"
)

// Error carries a Code.
type Error struct{ Code Code }

func (e *Error) Error() string { return "healthupload: " + string(e.Code) }

// CodeOf is err's code, or "" for an error this package did not make.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Identity is the machine's own Cloud identity: the control plane it was
// minted against and its credential. The credential leaves only as an
// Authorization header to IngestPath under APIBase.
type Identity struct {
	APIBase    string
	Credential string
}

// IdentityProvider answers the identity to upload with, read locally on
// every cycle so opt-out, Cloud off and revocation take effect without a
// restart. An off state is an *Error with one of the off codes.
type IdentityProvider func() (Identity, error)

// Source is the durable store.
type Source interface {
	Read(healthjournal.Query) (healthjournal.Page, error)
}

// Options configure an Uploader.
type Options struct {
	// StateDir holds the progress file.
	StateDir string
	Identity IdentityProvider
	// Source returns the durable store, or nil while it is not open.
	Source func() Source
	Now    func() time.Time
	// Transport replaces the default HTTPS transport (tests).
	Transport http.RoundTripper
	// AllowLoopbackHTTP admits an http:// API base on a loopback host. It
	// exists for isolated tests; the daemon never sets it.
	AllowLoopbackHTTP bool
	Log               func(format string, args ...any)
	// Wake, when set, is the idle interval (tests).
	Wake time.Duration
	// AttemptTimeout, when set below MaxAttemptSeconds, shortens one
	// request's deadline (tests).
	AttemptTimeout time.Duration
}

// Uploader is one daemon's health uploader.
type Uploader struct {
	o      Options
	client *http.Client

	// refusedCredential is the fingerprint of a credential Cloud refused;
	// it is not used again until the identity changes. Run's goroutine only.
	refusedCredential [32]byte

	mu     sync.Mutex
	status Status
	st     persisted
}

// New makes an uploader. It makes no request and starts nothing.
func New(o Options) *Uploader {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if o.Wake == 0 {
		o.Wake = idleInterval
	}
	if o.AttemptTimeout <= 0 || o.AttemptTimeout > MaxAttemptSeconds*time.Second {
		o.AttemptTimeout = MaxAttemptSeconds * time.Second
	}
	transport := o.Transport
	if transport == nil {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: MaxAttemptSeconds * time.Second,
			MaxConnsPerHost:       1,
			MaxIdleConns:          1,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	}
	u := &Uploader{o: o, client: &http.Client{
		Transport: transport,
		Timeout:   o.AttemptTimeout,
		// A redirect would carry the bearer to another place: never follow.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	u.status = Status{Schema: StatusSchema, State: CodeStopped, Since: o.Now().UTC(), Remote: RemoteScope}
	return u
}

// StatusSchema names the status shape.
const StatusSchema = "clawdline.connection-health.upload-status/1"

// RemoteScope says what leaves the machine.
const RemoteScope = "content_free_metadata_only"

// Status is the uploader as the local route reports it.
type Status struct {
	Schema string    `json:"schema"`
	State  Code      `json:"state"`
	Since  time.Time `json:"state_since"`
	// LastError is the most recent failure code, kept after recovery.
	LastError Code `json:"last_error,omitempty"`
	// LastRefusal is Cloud's code and field for the last refused batch,
	// never its message.
	LastRefusal *Refusal `json:"last_refusal,omitempty"`
	// Remote says only allowlisted metadata is sent; owner diagnostics
	// (location, age, deadline) are not uploaded.
	Remote  string         `json:"remote"`
	Cursor  string         `json:"cursor"`
	Pending *PendingStatus `json:"pending,omitempty"`
	Hour    HourStatus     `json:"hour"`
	Totals  Totals         `json:"totals"`
	// LastReceipt is the newest durable receipt. Accepted means Cloud
	// committed it; nothing here says a person has seen it.
	LastReceipt *Receipt `json:"last_receipt,omitempty"`
}

// Refusal is a typed Cloud refusal.
type Refusal struct {
	HTTP  int    `json:"http"`
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

// PendingStatus is the batch in flight.
type PendingStatus struct {
	BatchID     string    `json:"batch_id"`
	BootID      string    `json:"boot_id"`
	Events      int       `json:"events"`
	Bytes       int       `json:"bytes"`
	FirstSeq    uint64    `json:"first_seq"`
	LastSeq     uint64    `json:"last_seq"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	// Persisted is "unknown" once an attempt was sent without an answer.
	Persisted string `json:"persisted"`
}

// HourStatus is this clock hour's spending against the local budget.
type HourStatus struct {
	Start         time.Time `json:"start"`
	Bytes         int64     `json:"bytes"`
	Requests      int64     `json:"requests"`
	LimitBytes    int64     `json:"limit_bytes"`
	LimitRequests int64     `json:"limit_requests"`
}

// Status is a copy; it never waits on I/O.
func (u *Uploader) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.status
	s.Cursor = u.st.Cursor
	s.Totals = u.st.Totals
	s.Totals.Excluded = cloneMap(u.st.Totals.Excluded)
	s.Totals.Gaps = cloneMap(u.st.Totals.Gaps)
	s.Hour = HourStatus{Start: u.st.Hour.Start, Bytes: u.st.Hour.Bytes, Requests: u.st.Hour.Requests,
		LimitBytes: MaxHourlyRoutineBytes, LimitRequests: MaxHourlyRequests}
	if p := u.st.Pending; p != nil {
		persistedState := "no"
		if p.Attempts > 0 {
			persistedState = "unknown"
		}
		s.Pending = &PendingStatus{BatchID: p.BatchID, BootID: p.BootID, Events: p.Events, Bytes: len(p.Body),
			FirstSeq: p.FirstSeq, LastSeq: p.LastSeq, Attempts: p.Attempts, NextAttempt: p.NotBefore, Persisted: persistedState}
	}
	if n := len(u.st.Receipts); n > 0 {
		r := u.st.Receipts[n-1]
		s.LastReceipt = &r
	}
	return s
}

func cloneMap(m map[string]uint64) map[string]uint64 {
	if m == nil {
		return nil
	}
	out := make(map[string]uint64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (u *Uploader) set(c Code) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status.State != c {
		u.status.State, u.status.Since = c, u.o.Now().UTC()
	}
	switch c {
	case CodeIdle, CodeUploading, CodeNotOptedIn, CodeCloudOff, CodeNoIdentity, CodeSourceOpening, CodeStopped:
	default:
		u.status.LastError = c
	}
}

// Run uploads until ctx ends. It loads the progress file first; a file that
// cannot be read stops the uploader with that state rather than starting
// over.
func (u *Uploader) Run(ctx context.Context) {
	st, _, err := loadState(u.o.StateDir)
	if err != nil {
		u.set(CodeStateUnreadable)
		u.o.Log("connection health upload: the progress file could not be read; nothing will be uploaded until it is moved aside")
		<-ctx.Done()
		u.set(CodeStopped)
		return
	}
	u.mu.Lock()
	u.st = st
	u.mu.Unlock()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			u.set(CodeStopped)
			return
		case <-timer.C:
		}
		timer.Reset(u.Step(ctx))
	}
}

// Step runs one cycle and answers how long to wait before the next. It is
// exported for tests; Run is its only caller in the daemon.
func (u *Uploader) Step(ctx context.Context) time.Duration {
	id, err := u.o.Identity()
	if err != nil {
		c := CodeOf(err)
		if c == "" {
			c = CodeIdentityUnread
		}
		u.set(c)
		return u.o.Wake
	}
	endpoint, err := u.endpoint(id.APIBase)
	if err != nil {
		u.set(CodeOtherOrigin)
		return u.o.Wake
	}
	if id.Credential == "" {
		u.set(CodeNoIdentity)
		return u.o.Wake
	}
	if sha256.Sum256([]byte(id.Credential)) == u.refusedCredential {
		u.set(CodeAuthRefused)
		return u.o.Wake
	}
	now := u.o.Now()
	if u.pending() == nil {
		wait, ok := u.build(now)
		if !ok {
			return wait
		}
	}
	return u.attempt(ctx, endpoint, id, now)
}

func (u *Uploader) pending() *pendingBatch {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st.Pending
}

// endpoint validates the API base the identity belongs to.
func (u *Uploader) endpoint(base string) (string, error) {
	p, err := url.Parse(base)
	if err != nil || p.Host == "" || p.User != nil || p.RawQuery != "" || p.Fragment != "" || p.Opaque != "" {
		return "", errors.New("bad base")
	}
	switch p.Scheme {
	case "https":
	case "http":
		h := strings.ToLower(p.Hostname())
		if !u.o.AllowLoopbackHTTP || !(h == "127.0.0.1" || h == "::1" || h == "localhost") {
			return "", errors.New("not https")
		}
	default:
		return "", errors.New("bad scheme")
	}
	return strings.TrimRight(p.String(), "/") + IngestPath, nil
}

// build reads the store from the cursor and makes the next batch. ok is
// false when there is nothing to send; spans with nothing sendable settle
// locally (cursor and counts) without a request.
func (u *Uploader) build(now time.Time) (time.Duration, bool) {
	var src Source
	if u.o.Source != nil {
		src = u.o.Source()
	}
	if src == nil {
		u.set(CodeSourceOpening)
		return u.o.Wake, false
	}
	for round := 0; round < maxBuildRounds; round++ {
		u.mu.Lock()
		cursor := u.st.Cursor
		u.mu.Unlock()
		page, err := src.Read(healthjournal.Query{Cursor: cursor, Limit: pageLimit})
		if err != nil {
			if healthjournal.CodeOf(err) == healthjournal.CodeInvalidCursor {
				// The cursor's segment and offset no longer exist (a store
				// moved aside): start over from the oldest retained entry,
				// counted as a gap. Cloud's per-event digests answer
				// duplicate_event for anything already stored.
				u.mu.Lock()
				u.st.Cursor = ""
				u.st.Totals.add(skipped{Gaps: map[string]uint64{"invalid_cursor": 1}})
				u.mu.Unlock()
				continue
			}
			u.set(CodeSourceFailed)
			return u.o.Wake, false
		}
		b := assemble(page, cursor)
		if b.next == cursor {
			u.set(CodeIdle)
			return u.o.Wake, false
		}
		if len(b.events) == 0 {
			if err := u.commitLocal(b.next, b.skipped); err != nil {
				return stateRetry, false
			}
			continue
		}
		body, err := encode(wireBatch{SchemaVersion: WireSchemaVersion, BatchID: b.batchID(), ProducerInstance: ProducerInstance,
			BootID: b.boot, BuildCommit: b.build, Events: b.events})
		if err != nil || len(body) > MaxBatchBytes {
			u.set(CodeSourceFailed)
			return u.o.Wake, false
		}
		p := &pendingBatch{BatchID: b.batchID(), BootID: b.boot, Body: body, NextCursor: b.next, Events: len(b.events),
			FirstSeq: b.events[0].Seq, LastSeq: b.events[len(b.events)-1].Seq, Created: now.UTC(), Skipped: b.skipped}
		next := u.snapshot()
		next.Pending = p
		if err := u.save(next); err != nil {
			// Never send a batch whose bytes are not on disk: a restart
			// would rebuild it, and its attempts would be unbounded.
			return stateRetry, false
		}
		return 0, true
	}
	return 0, false
}

func (u *Uploader) snapshot() persisted {
	u.mu.Lock()
	defer u.mu.Unlock()
	p := u.st
	p.Totals.Excluded = cloneMap(p.Totals.Excluded)
	p.Totals.Gaps = cloneMap(p.Totals.Gaps)
	p.Receipts = append([]Receipt(nil), p.Receipts...)
	if p.Pending != nil {
		c := *p.Pending
		p.Pending = &c
	}
	return p
}

// save persists next and, only once it is on disk, makes it the state.
func (u *Uploader) save(next persisted) error {
	next.Schema = StateSchema
	if err := saveState(u.o.StateDir, next); err != nil {
		u.set(CodeStateUnwritable)
		return err
	}
	u.mu.Lock()
	u.st = next
	u.mu.Unlock()
	return nil
}

func (u *Uploader) commitLocal(next string, s skipped) error {
	p := u.snapshot()
	p.Cursor = next
	p.Totals.add(s)
	return u.save(p)
}

// span is one batch under construction.
type span struct {
	boot, build string
	events      []wireEvent
	bytes       int
	next        string
	skipped     skipped
}

func (b span) batchID() string {
	return "s" + strconv.FormatUint(b.events[0].Seq, 10) + "-" + strconv.FormatUint(b.events[len(b.events)-1].Seq, 10)
}

// assemble takes entries from a page while they fit one batch of one boot.
// next is the cursor after the last entry taken.
func assemble(page healthjournal.Page, cursor string) span {
	b := span{next: cursor}
	builds := map[string]string{}
	for _, boot := range page.Boots {
		builds[boot.BootID] = boot.Build
	}
	if g := page.Coverage.Gap; g != nil && g.Before == cursor && len(page.Entries) == 0 && g.Resume != "" && g.Resume != cursor {
		b.skipped.gap(g.Reason)
		b.next = g.Resume
		return b
	}
	if g := page.Coverage.Gap; g != nil && g.Before == cursor {
		b.skipped.gap(g.Reason)
	}
	envelope := len(`{"schema_version":1,"batch_id":"s18446744073709551615-18446744073709551615","producer_instance":"daemon","boot_id":"","build_commit":"","events":[]}`) + 2*64
	for _, e := range page.Entries {
		if b.boot != "" && e.BootID != b.boot && len(b.events) > 0 {
			break
		}
		switch e.Kind {
		case healthjournal.EntryLoss:
			if e.Loss != nil {
				b.skipped.StorageLoss += e.Loss.Records
			}
		case healthjournal.EntryRecord:
			if e.Record == nil {
				b.skipped.exclude(ExcludedPattern)
				break
			}
			w, reason := toWire(*e.Record)
			if reason != "" {
				b.skipped.exclude(reason)
				break
			}
			raw, _ := json.Marshal(w)
			if envelope+len(raw)+1 > MaxBatchBytes-headroomBytes {
				b.skipped.exclude(ExcludedTooLarge)
				break
			}
			if len(b.events) == MaxBatchEvents || envelope+b.bytes+len(raw)+1 > MaxBatchBytes-headroomBytes {
				return b
			}
			if b.boot == "" {
				b.boot = e.BootID
				if build := builds[e.BootID]; cloudOpaque.MatchString(build) {
					b.build = build
				}
			}
			b.events = append(b.events, w)
			b.bytes += len(raw) + 1
		}
		b.next = e.Cursor
	}
	if len(page.Entries) == 0 && page.Coverage.NextCursor != "" && page.Coverage.NextCursor != cursor && page.Coverage.Gap != nil {
		b.next = page.Coverage.NextCursor
	}
	return b
}

// attempt sends the pending batch once, if the bounds allow it.
func (u *Uploader) attempt(ctx context.Context, endpoint string, id Identity, now time.Time) time.Duration {
	p := u.pending()
	if p == nil {
		return u.o.Wake
	}
	if now.Before(p.NotBefore) {
		return p.NotBefore.Sub(now)
	}
	if p.Attempts >= MaxAttempts || (p.Attempts > 0 && now.Sub(p.Created) > MaxRetrySeconds*time.Second) {
		next := u.snapshot()
		next.Cursor, next.Pending = p.NextCursor, nil
		next.Totals.AbandonedBatches++
		next.Totals.AbandonedEvents += uint64(p.Events)
		next.Totals.add(p.Skipped)
		if u.save(next) != nil {
			return stateRetry
		}
		u.o.Log("connection health upload: batch %s abandoned after %d attempts; Cloud may or may not hold it", p.BatchID, p.Attempts)
		return 0
	}
	// The local budget, per clock hour, counted before sending.
	next := u.snapshot()
	hour := now.UTC().Truncate(time.Hour)
	if !next.Hour.Start.Equal(hour) {
		next.Hour = hourBudget{Start: hour}
	}
	if next.Hour.Requests+1 > MaxHourlyRequests || next.Hour.Bytes+int64(len(p.Body)) > MaxHourlyRoutineBytes {
		u.set(CodeQuotaLocal)
		return hour.Add(time.Hour).Sub(now)
	}
	next.Hour.Requests++
	next.Hour.Bytes += int64(len(p.Body))
	next.Pending.Attempts++
	next.Totals.Attempts++
	next.Totals.BytesSent += uint64(len(p.Body))
	// The attempt is counted on disk before it is made, so a crash loop
	// cannot reset the bound.
	if u.save(next) != nil {
		return stateRetry
	}
	u.set(CodeUploading)
	status, body, retryAfter, err := u.send(ctx, endpoint, id.Credential, p.Body)
	if ctx.Err() != nil {
		return 0
	}
	return u.settle(p, next.Pending.Attempts, status, body, retryAfter, err, id, now)
}

func (u *Uploader) send(ctx context.Context, endpoint, credential string, body []byte) (int, []byte, time.Duration, error) {
	actx, cancel := context.WithTimeout(ctx, u.o.AttemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	res, err := u.client.Do(req)
	if err != nil {
		// The error may quote the URL; it is not logged or stored, only
		// classified.
		return 0, nil, 0, errors.New("request failed")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxResponseBytes))
	if err != nil {
		return 0, nil, 0, errors.New("answer unreadable")
	}
	var ra time.Duration
	if s, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && s > 0 {
		ra = time.Duration(min(s, MaxRetryAfterSeconds)) * time.Second
	}
	return res.StatusCode, data, ra, nil
}

func backoff(attempts int) time.Duration {
	d := firstBackoff
	for i := 1; i < attempts && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// settle applies one answer. Only a parsed receipt for this batch moves the
// cursor as delivered; a refusal moves it as refused; anything else keeps
// the batch for a bounded retry.
func (u *Uploader) settle(p *pendingBatch, attempts, status int, body []byte, retryAfter time.Duration, sendErr error, id Identity, now time.Time) time.Duration {
	retry := func(c Code, wait time.Duration) time.Duration {
		next := u.snapshot()
		if next.Pending == nil {
			return 0
		}
		next.Pending.NotBefore = now.Add(wait).UTC()
		if u.save(next) != nil {
			return stateRetry
		}
		u.set(c)
		return wait
	}
	switch {
	case sendErr != nil || status >= 500 || status == 0:
		return retry(CodeUpstreamUnavailable, backoff(attempts))
	case status == http.StatusOK || status == http.StatusCreated:
		var rc receipt
		if json.Unmarshal(body, &rc) != nil || rc.SchemaVersion != WireSchemaVersion || rc.BatchID != p.BatchID ||
			rc.ReceiptID == "" || rc.Digest == "" || rc.Accepted == nil {
			// A 2xx without a receipt for this batch proves nothing.
			return retry(CodeUpstreamUnavailable, backoff(attempts))
		}
		dups := 0
		for _, r := range rc.Rejected {
			if r.Code == "duplicate_event" {
				dups++
			}
		}
		next := u.snapshot()
		next.Cursor, next.Pending = p.NextCursor, nil
		next.Totals.Batches++
		next.Totals.Receipted += uint64(*rc.Accepted)
		next.Totals.Duplicates += uint64(dups)
		next.Totals.add(p.Skipped)
		next.Receipts = append(next.Receipts, Receipt{ReceiptID: rc.ReceiptID, BatchID: rc.BatchID, BootID: p.BootID,
			Digest: rc.Digest, PersistedAt: rc.PersistedAt, RetainUntil: rc.RetainUntil, Accepted: *rc.Accepted,
			Duplicates: dups, FirstSeq: p.FirstSeq, LastSeq: p.LastSeq, Replay: status == http.StatusOK})
		if len(next.Receipts) > MaxReceipts {
			next.Receipts = next.Receipts[len(next.Receipts)-MaxReceipts:]
		}
		if u.save(next) != nil {
			// The receipt is Cloud's; the next attempt replays it (200).
			return stateRetry
		}
		u.set(CodeIdle)
		return 0
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		u.refusedCredential = sha256.Sum256([]byte(id.Credential))
		u.recordRefusal(status, body)
		// Not counted as a spent attempt against the batch's time: the
		// batch waits, unsent, for a credential Cloud accepts.
		next := u.snapshot()
		if next.Pending != nil {
			next.Pending.Attempts = max(0, next.Pending.Attempts-1)
			_ = u.save(next)
		}
		u.set(CodeAuthRefused)
		return u.o.Wake
	case status == http.StatusTooManyRequests:
		wait := retryAfter
		if wait == 0 {
			wait = backoff(attempts)
		}
		u.recordRefusal(status, body)
		return retry(CodeQuotaCloud, wait)
	default:
		// 400, 409, 413 and any other 4xx: this batch will never be
		// accepted as it is. It is counted, with Cloud's code and field,
		// and the next batch is built; nothing is resent.
		u.recordRefusal(status, body)
		next := u.snapshot()
		next.Cursor, next.Pending = p.NextCursor, nil
		next.Totals.RefusedBatches++
		next.Totals.RefusedEvents += uint64(p.Events)
		next.Totals.add(p.Skipped)
		if u.save(next) != nil {
			return stateRetry
		}
		u.set(CodeRefused)
		u.o.Log("connection health upload: Cloud refused batch %s (%d %s)", p.BatchID, status, u.Status().LastRefusal.Code)
		return 0
	}
}

func (u *Uploader) recordRefusal(status int, body []byte) {
	var r refusal
	_ = json.Unmarshal(body, &r)
	code := r.Error.Code
	if !cloudOpaque.MatchString(code) {
		code = fmt.Sprintf("http_%d", status)
	}
	field := r.Error.Details.Field
	if len(field) > 96 || strings.ContainsAny(field, " \"'/\\") {
		field = ""
	}
	u.mu.Lock()
	u.status.LastRefusal = &Refusal{HTTP: status, Code: code, Field: field}
	u.mu.Unlock()
}

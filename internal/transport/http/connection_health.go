package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// Durable store states as the local route reports them.
const (
	DurableOpening = "opening"
	DurableOpen    = "open"
	DurableFailed  = "failed"
)

// durableHealth is the durable store as the daemon opened it: immutable,
// swapped whole, so a reader never sees a store without its state.
type durableHealth struct {
	store *healthjournal.Store
	state string
	code  string
}

// SetConnectionHealthStore records the outcome of opening the durable
// store: opening (nil store, nil error, before the open returns), open, or
// failed with its typed code. It never holds a lock a reader waits on.
func (s *Server) SetConnectionHealthStore(st *healthjournal.Store, err error, opening bool) {
	switch {
	case opening:
		s.connectionHealthStore.Store(&durableHealth{state: DurableOpening})
	case err != nil:
		code := string(healthjournal.CodeOf(err))
		if code == "" {
			code = "internal"
		}
		s.connectionHealthStore.Store(&durableHealth{state: DurableFailed, code: code})
	default:
		s.connectionHealthStore.Store(&durableHealth{store: st, state: DurableOpen})
	}
}

// durableReport is the durable half of the status answer. A store that is
// not open says which of opening, failed or absent it is; never zeros.
type durableReport struct {
	State  string                `json:"state"`
	Code   string                `json:"code,omitempty"`
	Status *healthjournal.Status `json:"status,omitempty"`
}

func (s *Server) durableReport() durableReport {
	d := s.connectionHealthStore.Load()
	if d == nil {
		return durableReport{State: "not_opened"}
	}
	r := durableReport{State: d.state, Code: d.code}
	if d.store != nil {
		st := d.store.Status()
		r.Status = &st
	}
	return r
}

// SetConnectionHealthUpload hands the server the health uploader's status
// reader. It is a copy under the uploader's own short lock; an unreachable
// Cloud never makes it wait.
func (s *Server) SetConnectionHealthUpload(status func() healthupload.Status) {
	s.connectionHealthUpload.Store(&status)
}

// uploadReport is the uploader half of the status answer: `not_started`
// when this daemon has no uploader, never an empty status.
type uploadReport struct {
	State  string               `json:"state"`
	Status *healthupload.Status `json:"status,omitempty"`
}

func (s *Server) uploadReport() uploadReport {
	f := s.connectionHealthUpload.Load()
	if f == nil {
		return uploadReport{State: "not_started"}
	}
	st := (*f)()
	return uploadReport{State: string(st.State), Status: &st}
}

// SetConnectionHealth hands the server the daemon's per-boot connection-health
// runtime. Nil is monitoring turned off: the route then answers
// `unsupported`, never an empty healthy page. It is called once, before the
// server starts serving or measuring.
func (s *Server) SetConnectionHealth(rt *connectionhealth.Runtime) { s.connectionHealth = rt }

// ConnectionHealth is the runtime SetConnectionHealth installed, or nil.
func (s *Server) ConnectionHealth() *connectionhealth.Runtime { return s.connectionHealth }

// connectionHealthRoute answers GET /v1/connection-health/{status,events,
// trace,export} from the in-memory runtime (docs/admin-connection-health-
// contract.md). It reads atomics and the journal's own short lock only: it
// never calls Scan, the Cloud line, the relay or a status snapshot, so a
// stuck handler or a dead Cloud cannot hold it, and holding it cannot hold
// them.
//
// Only this machine's own token reads it, and never through the Cloud line:
// location codes, ages and correlations are owner diagnostics (contract §8),
// and the hosted console reaches this daemon with this machine's token in
// process, so the local-token check alone would let it through.
func (s *Server) connectionHealthRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "Connection health is read with GET.")
		return
	}
	if cloud.ViaCloud(r.Context()) {
		writeRefusal(w, http.StatusForbidden, string(connectionhealth.CodeForbidden),
			"Connection health is read on this machine only, not through Cloud.")
		return
	}
	if !requireLocal(w, r) {
		return
	}
	rt := s.connectionHealth
	if rt == nil {
		writeRefusal(w, http.StatusNotImplemented, string(connectionhealth.CodeUnsupported),
			"Connection health monitoring is off in this daemon.")
		return
	}
	q := r.URL.Query()
	switch strings.TrimPrefix(r.URL.Path, "/v1/connection-health/") {
	case "status":
		st, err := rt.Status()
		if err != nil {
			writeConnectionHealthError(w, err)
			return
		}
		writeJSON(w, struct {
			connectionhealth.Status
			Durable durableReport `json:"durable"`
			Upload  uploadReport  `json:"upload"`
		}{st, s.durableReport(), s.uploadReport()})
	case "history":
		s.connectionHealthHistory(w, q.Get("cursor"), q.Get("limit"))
	case "events":
		query, since, until, err := connectionHealthWindow(q)
		if err != nil {
			writeConnectionHealthError(w, err)
			return
		}
		page, err := rt.Events(query, since, until)
		if err != nil {
			writeConnectionHealthError(w, err)
			return
		}
		writeJSON(w, page)
	case "trace":
		tr, err := rt.Trace(q.Get("correlation"))
		if err != nil {
			writeConnectionHealthError(w, err)
			return
		}
		writeJSON(w, tr)
	case "export":
		query, since, until, err := connectionHealthWindow(q)
		if err != nil {
			writeConnectionHealthError(w, err)
			return
		}
		ev, err := rt.Export(query)
		if err == nil && (!since.IsZero() || !until.IsZero()) {
			ev.Journal, err = rt.Events(query, since, until)
		}
		if err != nil {
			writeConnectionHealthError(w, err)
			return
		}
		writeJSON(w, ev)
	default:
		writeNoSuchRoute(w, r)
	}
}

// connectionHealthWindow reads boot_id, after_seq, limit, since and until.
// A cursor that is not a number, or does not fit one, is invalid_cursor; a
// limit or a time that does not parse names its field.
func connectionHealthWindow(v map[string][]string) (connectionhealth.Query, time.Time, time.Time, error) {
	get := func(k string) string {
		if xs := v[k]; len(xs) > 0 {
			return xs[0]
		}
		return ""
	}
	var q connectionhealth.Query
	var since, until time.Time
	q.BootID = get("boot_id")
	if s := get("after_seq"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return q, since, until, &connectionhealth.Error{Code: connectionhealth.CodeInvalidCursor, Field: "after_seq"}
		}
		q.AfterSeq = n
	}
	if s := get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return q, since, until, &connectionhealth.Error{Code: connectionhealth.CodeQueryBoundExceeded, Field: "limit"}
		}
		q.Limit = n
	}
	for _, f := range []struct {
		name string
		to   *time.Time
	}{{"since", &since}, {"until", &until}} {
		if s := get(f.name); s != "" {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return q, since, until, &connectionhealth.Error{Code: connectionhealth.CodeInvalidField, Field: f.name}
			}
			*f.to = t
		}
	}
	return q, since, until, nil
}

// writeConnectionHealthError answers a typed failure with its code; the
// detail is the code and field only, never a value from the request.
func writeConnectionHealthError(w http.ResponseWriter, err error) {
	code := connectionhealth.CodeOf(err)
	status := http.StatusBadRequest
	switch code {
	case connectionhealth.CodeCursorOtherBoot:
		status = http.StatusConflict
	case connectionhealth.CodeUnsupported:
		status = http.StatusServiceUnavailable
	case "":
		status, code = http.StatusInternalServerError, "internal"
	}
	writeRawRefusal(w, status, string(code), err.Error())
}

// connectionHealthHistory pages the durable store: this boot's records and
// every earlier boot's still retained, with its own cursor and coverage. A
// store that is not open is unsupported with the state it is in, never an
// empty history.
func (s *Server) connectionHealthHistory(w http.ResponseWriter, cursor, limit string) {
	d := s.connectionHealthStore.Load()
	if d == nil || d.store == nil {
		state, code := "not_opened", ""
		if d != nil {
			state, code = d.state, d.code
		}
		detail := "The durable connection-health store is " + state
		if code != "" {
			detail += " (" + code + ")"
		}
		writeRawRefusal(w, http.StatusServiceUnavailable, string(connectionhealth.CodeUnsupported), detail+".")
		return
	}
	n := 0
	if limit != "" {
		v, err := strconv.Atoi(limit)
		if err != nil {
			writeRawRefusal(w, http.StatusBadRequest, string(connectionhealth.CodeQueryBoundExceeded), "limit")
			return
		}
		n = v
	}
	page, err := d.store.Read(healthjournal.Query{Cursor: cursor, Limit: n})
	if err != nil {
		code := healthjournal.CodeOf(err)
		status := http.StatusBadRequest
		switch code {
		case healthjournal.CodeReadersBusy, healthjournal.CodeClosed:
			status = http.StatusServiceUnavailable
		case "":
			status, code = http.StatusInternalServerError, "internal"
		}
		writeRawRefusal(w, status, string(code), err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func healthRunAgainst(t *testing.T, handler http.HandlerFunc, args ...string) (int, string, string, *http.Request) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	var asked *http.Request
	newRequest := func(path string, q url.Values) (*http.Request, error) {
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer local-token")
		}
		asked = req
		return req, err
	}
	var stdout, stderr bytes.Buffer
	code := healthRun(&stdout, &stderr, args, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), newRequest, srv.Client().Do)
	return code, stdout.String(), stderr.String(), asked
}

// Usage mistakes exit 2 before any request: an unknown subcommand, a cursor
// that does not fit a sequence number, a limit past the page bound, a time
// that is neither RFC 3339 nor a duration, a correlation outside the opaque
// alphabet.
func TestHealthCommandRefusesBadArgumentsBeforeAsking(t *testing.T) {
	never := func(w http.ResponseWriter, r *http.Request) { t.Errorf("asked %s", r.URL) }
	for _, args := range [][]string{
		nil, {"nope"}, {"events", "--after-seq", "18446744073709551616"}, {"events", "--after-seq", "-1"},
		{"events", "--limit", "201"}, {"export", "--since", "yesterday"}, {"trace", "--correlation", "../etc"},
		{"trace"},
	} {
		if code, _, _, _ := healthRunAgainst(t, never, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

// The window is sent as RFC 3339 times, a duration counted back from now.
func TestHealthCommandSendsTheWindow(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"records":[]}`) }
	code, out, _, req := healthRunAgainst(t, ok, "events", "--since", "15m", "--until", "2026-10-20T12:00:00Z", "--after-seq", "3", "--limit", "5")
	if code != 0 || !strings.Contains(out, `"records": []`) {
		t.Fatalf("exit %d: %s", code, out)
	}
	q := req.URL.Query()
	if req.URL.Path != "/v1/connection-health/events" || q.Get("since") != "2026-10-20T11:45:00Z" ||
		q.Get("until") != "2026-10-20T12:00:00Z" || q.Get("after_seq") != "3" || q.Get("limit") != "5" {
		t.Fatalf("asked %s", req.URL)
	}
}

// A refusal prints its code and exits 1; an unreachable daemon says the
// state is unknown, never that it is fine.
func TestHealthCommandSaysRefusedOrUnknown(t *testing.T) {
	refuse := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"forbidden","detail":"Only this machine's own token may manage devices."}`)
	}
	code, _, errOut, _ := healthRunAgainst(t, refuse, "status")
	if code != 1 || !strings.Contains(errOut, "403 forbidden") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var stderr bytes.Buffer
	down := func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") }
	newRequest := func(path string, _ url.Values) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "http://127.0.0.1:1"+path, nil)
	}
	if code := healthRun(io.Discard, &stderr, []string{"status"}, time.Now(), newRequest, down); code != 1 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("unreachable: exit %d: %s", code, stderr.String())
	}
}

// The status summary leads with the quality and lists only the findings
// that are slow or stuck, with their stage, location and age.
func TestHealthStatusSummary(t *testing.T) {
	body := `{"schema":"clawdline.connection-health.status/1","boot_id":"boot-1","build_commit":"abc","running":true,
	"quality":"complete","journal_coverage":{"quality":"complete","first_retained_seq":1,"last_seq":3,"evicted_total":0,"refused_total":0,"truncated":false,"next_after_seq":0,"durability":"memory_only_lost_on_restart"},
	"watchdog":{"rule":"watchdog/1","scan_seq":9,"trace_coverage":"complete","transitions":"complete","overflow":{"total":0,"critical":0},
	"findings":[{"phase_id":2,"stage":"handler_started","location":"cloud.link.read_handler","state":"blocked","age_ns":16000000000,"correlation":"op-2"},
	{"phase_id":1,"stage":"idle","location":"cloud.transport.read_loop","state":"idle","age_ns":1}]}}`
	code, out, errOut, _ := healthRunAgainst(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }, "status")
	if code != 0 || !strings.Contains(out, "quality complete") || !strings.Contains(out, "cloud.link.read_handler") ||
		!strings.Contains(out, "16s") || strings.Contains(out, "read_loop") {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
}

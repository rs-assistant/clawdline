package http

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/sockowner"
	"github.com/sainteye/clawdline/internal/adapters/sockowner/othersock"
)

// peerServer is the gate fixture behind a real listener, served by the
// http.Server Serve uses.
func peerServer(t *testing.T) (*gateFixture, *httptest.Server) {
	t.Helper()
	f, h := newGateFixture(t)
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	t.Cleanup(ts.Close)
	return f, ts
}

// asUser is a client whose every connection is a socket uid owns.
func asUser(t *testing.T, uid int) *http.Client {
	t.Helper()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			return othersock.Dial(uid, netip.MustParseAddrPort(addr))
		},
	}}
}

func send(t *testing.T, c *http.Client, method, url string, headers map[string]string, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

// The review's finding: a browser hands the console's cookie to every
// 127.0.0.1 port, so another user's service has it and replays it from its
// own process. Every credential from another user's socket is refused; the
// same credentials from this user's socket are let in; and what anybody
// without a credential may have is still answered.
func TestACredentialFromAnotherUsersProcessIsRefused(t *testing.T) {
	if _, err := othersock.Dial(othersock.Nobody, netip.MustParseAddrPort("127.0.0.1:1")); errors.Is(err, othersock.ErrUnavailable) {
		t.Skip(err)
	}
	f, ts := peerServer(t)
	other := asUser(t, othersock.Nobody)
	mine := ts.Client()
	credentialed := []struct {
		name, method, path, body string
		headers                  map[string]string
	}{
		{"the console's cookie", http.MethodGet, "/v1/diagnostics", "", map[string]string{"Cookie": "clawdline-next=" + f.send}},
		{"this machine's token", http.MethodGet, "/v1/diagnostics", "", map[string]string{"Authorization": "Bearer " + f.local}},
		{"the orchestrator's token", http.MethodGet, "/v1/board", "", map[string]string{machineHeader: f.machine}},
		{"a cookie on an open route", http.MethodGet, "/v1/health", "", map[string]string{"Cookie": "clawdline-next=" + f.send}},
		{"a token handed to adopt", http.MethodPost, "/v1/auth/adopt", `{"token":"` + f.send + `"}`,
			map[string]string{"Content-Type": "application/json"}},
		{"a task secret", http.MethodPost, "/v1/orchestrator/tasks/t1/complete", `{}`,
			map[string]string{"Content-Type": "application/json", "X-Clawdline-Task-Secret": "s"}},
	}
	for _, tc := range credentialed {
		t.Run(tc.name, func(t *testing.T) {
			status, body := send(t, other, tc.method, ts.URL+tc.path, tc.headers, tc.body)
			if status != http.StatusForbidden || !strings.Contains(body, "another user") {
				t.Fatalf("another user's process: %d %s; want 403 naming another user", status, body)
			}
			if status, body := send(t, mine, tc.method, ts.URL+tc.path, tc.headers, tc.body); status != http.StatusOK || body != "reached" {
				t.Fatalf("this user's process: %d %s; want it let through", status, body)
			}
		})
	}
	for _, tc := range []struct{ name, method, path, body string }{
		{"the console's page", http.MethodGet, "/", ""},
		{"a pairing request", http.MethodPost, "/v1/auth/pair", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{"Content-Type": "application/json"}
			if status, body := send(t, other, tc.method, ts.URL+tc.path, headers, tc.body); status != http.StatusOK || body != "reached" {
				t.Fatalf("without a credential: %d %s; want what anybody is answered", status, body)
			}
		})
	}
}

// When /proc cannot say who is asking, a credential on a loopback connection
// is refused and the refusal says so — but the console's own files still
// load, and a request answered in process (Cloud, a test) has no connection
// to judge and is let in by its credential as before.
func TestAPeerNobodyCanIdentifyIsRefusedAndToldWhy(t *testing.T) {
	old := socketOwner
	socketOwner = func(local, remote netip.AddrPort) (uint32, error) {
		return 0, errors.New("open /proc/net/tcp: permission denied")
	}
	defer func() { socketOwner = old }()
	f, ts := peerServer(t)
	mine := ts.Client()
	status, body := send(t, mine, http.MethodGet, ts.URL+"/v1/diagnostics", map[string]string{"Authorization": "Bearer " + f.local}, "")
	if status != http.StatusForbidden || !strings.Contains(body, "could not tell which user") {
		t.Fatalf("an unidentified peer with a credential: %d %s; want 403 saying why", status, body)
	}
	if status, body := send(t, mine, http.MethodGet, ts.URL+"/", map[string]string{"Cookie": "clawdline-next=" + f.send}, ""); status != http.StatusOK {
		t.Fatalf("the console's page: %d %s; want it served", status, body)
	}
	h := f.g.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "reached") }))
	rec := call{method: http.MethodGet, path: "/v1/diagnostics", headers: map[string]string{"Authorization": "Bearer " + f.local}}.do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("an in-process request: %d %s; want it judged by its credential", rec.Code, rec.Body.String())
	}
}

// A connection from another machine has no socket in this machine's table:
// it is a remote asker, judged by its credential. The same absence on
// loopback is a socket that should have been there, and is refused.
func TestOnlyALoopbackPeerMustBeFound(t *testing.T) {
	old := socketOwner
	socketOwner = func(local, remote netip.AddrPort) (uint32, error) { return 0, sockowner.ErrNotFound }
	defer func() { socketOwner = old }()
	server := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 7727}
	if v := judgePeer(server, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 50000}, 1000); !v.admit || v.kind != peerRemote {
		t.Fatalf("another machine: %+v; want a remote asker", v)
	}
	loop := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7727}
	if v := judgePeer(loop, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}, 1000); v.admit {
		t.Fatalf("a loopback socket that is not in the table: %+v; want refused", v)
	}
	socketOwner = func(local, remote netip.AddrPort) (uint32, error) { return 0, sockowner.ErrUnsupported }
	if v := judgePeer(loop, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}, 1000); !v.admit {
		t.Fatalf("a platform that cannot tell: %+v; want admitted", v)
	}
}

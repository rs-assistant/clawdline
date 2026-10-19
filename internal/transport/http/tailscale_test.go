package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/contract"
)

const tailscaleName = "mac.tailnet-example.ts.net"

// tailscaleRequest is a request the way Tailscale Serve hands one to this
// daemon: from loopback, with the phone's Host, and the forwarding headers
// Serve adds after terminating HTTPS.
func tailscaleRequest(method, path, body string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, "http://127.0.0.1:7757"+path, strings.NewReader(body))
	if body == "" {
		req.Body = http.NoBody
		req.ContentLength = 0
	}
	req.Host = tailscaleName
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-For", "192.0.2.10")
	req.Header.Set("X-Forwarded-Host", tailscaleName)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// writeRemoteSettings changes the settings through this machine's own route,
// as the settings window does.
func writeRemoteSettings(t *testing.T, h http.Handler, local string, keys map[string]any) {
	t.Helper()
	body, _ := json.Marshal(keys)
	rec := call{path: "/v1/settings", body: string(body), headers: map[string]string{
		"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings %s: %d %s", body, rec.Code, rec.Body)
	}
}

// A phone on the tailnet reaches this machine through Serve's address only
// while remote access is on and the name is set, pairs exactly as it would
// at the desk, and is a paired device there — never this machine.
func TestTailscaleServeAddressIsARemoteDoor(t *testing.T) {
	p := shellPane("")
	s, h, local, machine := wholeServer(t, p, p)
	g := s.gate()
	origin := "https://" + tailscaleName

	// Before anything is set, the name is somebody else's.
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/", "", nil)); rec.Code != http.StatusForbidden {
		t.Fatalf("with no Tailscale name: %d", rec.Code)
	}
	// A name with remote access off admits nothing either.
	writeRemoteSettings(t, h, local, map[string]any{"remote": false, "tailscale_hostname": tailscaleName})
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/", "", nil)); rec.Code != http.StatusForbidden {
		t.Fatalf("with remote access off: %d", rec.Code)
	}
	writeRemoteSettings(t, h, local, map[string]any{"remote": true})

	// The door is drawn for anybody who reaches it.
	door := serve(h, tailscaleRequest(http.MethodGet, "/", "", nil))
	here := call{method: http.MethodGet, path: "/"}.do(h)
	if door.Code != here.Code || door.Code == http.StatusForbidden {
		t.Fatalf("the door through Tailscale answered %d, at the desk %d", door.Code, here.Code)
	}
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/v1/health", "", nil)); rec.Code != http.StatusOK {
		t.Fatalf("health through Tailscale: %d", rec.Code)
	}
	// Somebody who has not paired reads nothing behind it.
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/v1/sessions", "", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unpaired read of the sessions: %d %s", rec.Code, rec.Body)
	}
	// This machine's own credentials do not travel: through Serve every
	// request comes from loopback, and the Host is what says it came from
	// the tailnet.
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/v1/auth/devices", "", map[string]string{
		"Authorization": "Bearer " + local})); rec.Code != http.StatusForbidden {
		t.Fatalf("the local token through Tailscale read a local-only route: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/v1/orchestrator/tasks", "", map[string]string{
		machineHeader: machine})); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the orchestrator token through Tailscale: %d %s", rec.Code, rec.Body)
	}
	// A turn's report stays on this machine's own browser.
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/reports/"+reportID, "", map[string]string{
		"Authorization": "Bearer " + local})); rec.Code != http.StatusForbidden {
		t.Fatalf("a report through Tailscale: %d", rec.Code)
	}

	// Pairing: the phone asks, this machine shows the code, the phone types it.
	notices, cancel := g.auth.Watch()
	defer cancel()
	rec := serve(h, tailscaleRequest(http.MethodPost, "/v1/auth/pair", `{"name":"phone"}`, map[string]string{
		"Origin": origin, "Content-Type": "application/json"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	var started contract.PairStarted
	_ = json.Unmarshal(rec.Body.Bytes(), &started)
	var code string
	select {
	case n := <-notices:
		code = n.Code
	case <-time.After(5 * time.Second):
		t.Fatal("no pairing code reached this machine")
	}
	body, _ := json.Marshal(map[string]string{"pairing_id": started.PairingID, "code": code})
	rec = serve(h, tailscaleRequest(http.MethodPost, "/v1/auth/pair/confirm", string(body), map[string]string{
		"Origin": origin, "Content-Type": "application/json"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}
	if set := rec.Header().Get("Set-Cookie"); !strings.Contains(set, "; Secure") || !strings.Contains(set, "HttpOnly") {
		t.Fatalf("the cookie set over Serve's HTTPS is %q", set)
	}
	var signed contract.SignedIn
	_ = json.Unmarshal(rec.Body.Bytes(), &signed)
	cookie := "clawdline-next=" + signed.Token

	// Paired, it reads and sends.
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/v1/sessions", "", map[string]string{"Cookie": cookie})); rec.Code != http.StatusOK {
		t.Fatalf("paired read: %d %s", rec.Code, rec.Body)
	}
	const board = `{"operation":"note","requestId":"r","expectedRevision":0,"actor":"t"}`
	write := func(headers map[string]string) int {
		return serve(h, tailscaleRequest(http.MethodPost, "/v1/board", board, headers)).Code
	}
	sent := write(map[string]string{"Origin": origin, "Sec-Fetch-Site": "same-origin", "Cookie": cookie, "Content-Type": "application/json"})
	if sent == http.StatusUnauthorized || sent == http.StatusForbidden || sent == http.StatusUnsupportedMediaType {
		t.Fatalf("paired write through Tailscale: %d", sent)
	}
	// A page on another origin, or on plain http, is not this one.
	for _, other := range []string{"https://evil.tailnet-example.ts.net", "http://" + tailscaleName, "http://127.0.0.1:7757"} {
		if got := write(map[string]string{"Origin": other, "Cookie": cookie, "Content-Type": "application/json"}); got != http.StatusForbidden {
			t.Errorf("a write from %s answered %d", other, got)
		}
	}
	// Another machine's name on the tailnet is not this one's.
	other := tailscaleRequest(http.MethodGet, "/v1/sessions", "", map[string]string{"Cookie": cookie})
	other.Host = "other.tailnet-example.ts.net"
	if rec := serve(h, other); rec.Code != http.StatusForbidden {
		t.Errorf("another tailnet name: %d", rec.Code)
	}

	// Revoking the device takes effect on its next request.
	var id string
	for _, d := range g.auth.Devices() {
		if d.Name == "phone" {
			id = d.ID
		}
	}
	if id == "" {
		t.Fatal("the paired phone is not in the device list")
	}
	if err := g.auth.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/v1/sessions", "", map[string]string{"Cookie": cookie})); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked read: %d", rec.Code)
	}

	// Turning remote access off, or clearing the name, closes the address on
	// the next request while Serve itself keeps running.
	writeRemoteSettings(t, h, local, map[string]any{"remote": false})
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/", "", nil)); rec.Code != http.StatusForbidden {
		t.Fatalf("after remote off: %d", rec.Code)
	}
	writeRemoteSettings(t, h, local, map[string]any{"remote": true})
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/", "", nil)); rec.Code == http.StatusForbidden {
		t.Fatalf("after remote on again: %d", rec.Code)
	}
	writeRemoteSettings(t, h, local, map[string]any{"tailscale_hostname": ""})
	if rec := serve(h, tailscaleRequest(http.MethodGet, "/", "", nil)); rec.Code != http.StatusForbidden {
		t.Fatalf("after the name was cleared: %d", rec.Code)
	}
}

// The name is the full MagicDNS name and nothing else.
func TestTailscaleHostnameIsAFullTailnetName(t *testing.T) {
	for _, ok := range []string{"", tailscaleName, "a-b.c-d.ts.net"} {
		if !nextconfig.ValidTailscaleHostname(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{
		"Machine.tailnet-example.ts.net", "https://" + tailscaleName, tailscaleName + ":443",
		tailscaleName + "/", "tailnet.ts.net", "mac.example.com", "mac.tailnet.ts.net.evil.com", "mac..ts.net",
	} {
		if nextconfig.ValidTailscaleHostname(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	p := shellPane("")
	_, h, local, _ := wholeServer(t, p, p)
	rec := call{path: "/v1/settings", body: `{"tailscale_hostname":"https://` + tailscaleName + `"}`, headers: map[string]string{
		"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_tailscale_hostname") {
		t.Fatalf("a URL as the name: %d %s", rec.Code, rec.Body)
	}
}

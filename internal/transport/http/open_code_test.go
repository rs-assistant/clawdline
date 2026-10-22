package http

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// openFixture is the auth routes behind the gate, with this machine's token.
func openFixture(t *testing.T) (*gate, http.Handler, string) {
	t.Helper()
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "next"), Port: 7757}}
	g := s.gate()
	if g.err != nil {
		t.Fatal(g.err)
	}
	local, err := g.auth.LocalToken()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/", s.authRoute)
	return g, g.wrap(mux), local
}

func openBrowser(t *testing.T, h http.Handler, local string) contract.BrowserDevice {
	t.Helper()
	rec := call{path: "/v1/auth/devices/browser", body: `{}`, headers: map[string]string{
		"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("browser device: %d %s", rec.Code, rec.Body)
	}
	var made contract.BrowserDevice
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	return made
}

func adopt(h http.Handler, body string) (int, string, string) {
	rec := call{path: "/v1/auth/adopt", body: body, headers: map[string]string{
		"Content-Type": "application/json", "Origin": "http://127.0.0.1:7757"}}.do(h)
	return rec.Code, rec.Header().Get("Set-Cookie"), rec.Body.String()
}

// The review's finding: `clawdline open` gave xdg-open an address holding the
// browser device's token, and every user on the machine can read a command
// line. The address must hold no long-lived credential: a code that signs in
// once, and not after MaxOpenCodeSeconds.
func TestTheOpenAddressCarriesAOneTimeCodeAndNoToken(t *testing.T) {
	g, h, local := openFixture(t)
	made := openBrowser(t, h, local)
	if strings.Contains(made.URL, made.Token) || strings.Contains(made.URL, "#t=") {
		t.Fatalf("the address carries the device's token: %s", made.URL)
	}
	_, code, ok := strings.Cut(made.URL, "#c=")
	if !ok || code == "" || !strings.HasPrefix(made.URL, "http://127.0.0.1:7757/v1/auth/open#c=") {
		t.Fatalf("the address carries no code: %s", made.URL)
	}
	if g.auth.Verify(code).Allowed {
		t.Fatal("the code itself is a credential")
	}

	status, cookie, body := adopt(h, `{"code":"`+code+`"}`)
	if status != http.StatusOK || !strings.Contains(cookie, "clawdline-next="+made.Token+";") {
		t.Fatalf("trading the code: %d %q %s", status, cookie, body)
	}
	if strings.Contains(body, made.Token) {
		t.Fatalf("the page was handed the token: %s", body)
	}
	if status, cookie, _ := adopt(h, `{"code":"`+code+`"}`); status != http.StatusUnauthorized || cookie != "" {
		t.Fatalf("the same code a second time: %d %q; want refused", status, cookie)
	}

	// One left unopened lapses.
	now := time.Now()
	g.opens.now = func() time.Time { return now }
	late := openBrowser(t, h, local)
	_, lateCode, _ := strings.Cut(late.URL, "#c=")
	g.opens.now = func() time.Time { return now.Add(MaxOpenCodeSeconds * time.Second) }
	if status, cookie, _ := adopt(h, `{"code":"`+lateCode+`"}`); status != http.StatusUnauthorized || cookie != "" {
		t.Fatalf("a code %d seconds old: %d %q; want refused", MaxOpenCodeSeconds, status, cookie)
	}

	// A device revoked before its address was opened does not sign in.
	g.opens.now = nil
	revoked := openBrowser(t, h, local)
	_, revokedCode, _ := strings.Cut(revoked.URL, "#c=")
	if err := g.auth.Revoke(revoked.ID); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := adopt(h, `{"code":"`+revokedCode+`"}`); status != http.StatusUnauthorized {
		t.Fatalf("a revoked device's code: %d; want refused", status)
	}

	// An address made before this change still signs in.
	if status, cookie, _ := adopt(h, `{"token":"`+made.Token+`"}`); status != http.StatusOK || cookie == "" {
		t.Fatalf("adopting a token: %d %q", status, cookie)
	}
}

// At most MaxOpenCodesPending addresses wait; past that `open` is refused and
// no device is added.
func TestTooManyUnopenedAddressesAreRefused(t *testing.T) {
	g, h, local := openFixture(t)
	for i := 0; i < MaxOpenCodesPending; i++ {
		openBrowser(t, h, local)
	}
	before := len(g.auth.Devices())
	rec := call{path: "/v1/auth/devices/browser", body: `{}`, headers: map[string]string{
		"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("one past the limit: %d %s", rec.Code, rec.Body)
	}
	if after := len(g.auth.Devices()); after != before {
		t.Fatalf("a refused address added a device: %d → %d", before, after)
	}
}

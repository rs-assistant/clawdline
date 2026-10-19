package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// The tunnel's wiring: who may read it, and that it follows the devices — the
// refusal to open while nothing is paired is decided on the gate's own
// authority at the moment of each change, so a pairing brings it up and the
// revocation that leaves nothing paired takes it down.
//
// cloudflared here is a shell script named in `cloudflared_path`, which is
// looked at before anywhere a real one lives; it answers the credentials
// listing and prints what a real named tunnel prints, and reaches nothing.
func TestTunnelFollowsTheDevices(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake cloudflared is a shell script")
	}
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "cloudflared")
	script := "#!/bin/sh\n" +
		"if [ \"$4\" = list ]; then echo '[{\"id\":\"0b1c2d3e\",\"name\":\"clawdline\"}]'; exit 0; fi\n" +
		"printf '%s\\n' 'INF Registered tunnel connection connIndex=0 location=tpe01' >&2\n" +
		"trap 'exit 0' TERM\nwhile :; do sleep 0.05 >/dev/null 2>&1; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(map[string]any{
		"remote": true, "remote_tunnel": "named", "remote_tunnel_name": "clawdline",
		"remote_hostname": "box.example.com", "cloudflared_path": fake,
	})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}}
	t.Cleanup(s.StopTunnel)
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
	mux.HandleFunc("/v1/tunnel", s.tunnelRoute)
	h := g.wrap(mux)
	status := func() contract.TunnelStatus {
		t.Helper()
		rec := call{method: http.MethodGet, path: "/v1/tunnel", headers: map[string]string{"Authorization": "Bearer " + local}}.do(h)
		if rec.Code != http.StatusOK {
			t.Fatalf("tunnel status: %d %s", rec.Code, rec.Body)
		}
		var st contract.TunnelStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	waitState := func(want contract.TunnelState) contract.TunnelStatus {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			st := status()
			if st.State == want {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("waited for %s; the tunnel is %+v", want, st)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Who may read it: this machine's own token and nothing else.
	if rec := (call{method: http.MethodGet, path: "/v1/tunnel"}).do(h); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := (call{method: http.MethodGet, path: "/v1/tunnel", headers: map[string]string{"Authorization": "Bearer not-a-token"}}).do(h); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a token that is not ours: %d", rec.Code)
	}

	s.StartTunnel()
	st := status()
	if st.State != contract.TunnelStateFailed || !strings.Contains(st.Reason, "no paired device") || len(st.Command) != 0 {
		t.Fatalf("with nothing paired: %+v", st)
	}

	// A browser device, made through the route `clawdline open` uses: now
	// somebody is let in, and the tunnel follows without being asked.
	rec := call{path: "/v1/auth/devices/browser", body: `{}`, headers: map[string]string{
		"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("browser device: %d %s", rec.Code, rec.Body)
	}
	var browser contract.BrowserDevice
	_ = json.Unmarshal(rec.Body.Bytes(), &browser)
	if verdict := g.auth.Verify(browser.Token); !verdict.Allowed || !verdict.Caps.Has(auth.Read) || !verdict.Caps.Has(auth.Send) {
		t.Fatalf("new browser cannot read and send: %+v", verdict)
	}
	st = waitState(contract.TunnelStateUp)
	if st.URL != "https://box.example.com" || st.Mode != contract.TunnelModeNamed {
		t.Fatalf("up: %+v", st)
	}
	joined := strings.Join(st.Command, " ")
	if !strings.HasPrefix(joined, fake+" tunnel --config "+filepath.Join(dir, "cloudflared.yml")+" ") ||
		!strings.HasSuffix(joined, " run clawdline") {
		t.Fatalf("command: %s", joined)
	}
	// A paired device reads the rest of the machine, and not this.
	rec = call{method: http.MethodGet, path: "/v1/tunnel", headers: map[string]string{"Authorization": "Bearer " + browser.Token}}.do(h)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "cloudflared") {
		t.Fatalf("a paired device read the tunnel: %d %s", rec.Code, rec.Body)
	}

	// Revoking everything leaves nothing paired, and the tunnel goes with it.
	rec = call{path: "/v1/auth/devices/revoke-all", body: `{}`, headers: map[string]string{
		"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke-all: %d %s", rec.Code, rec.Body)
	}
	st = status()
	if st.State != contract.TunnelStateFailed || !strings.Contains(st.Reason, "no paired device") || st.URL != "" {
		t.Fatalf("after revoking everything: %+v", st)
	}
}

// A settings file written before the quick tunnel was removed still starts the
// daemon and still opens the settings page: `quick` reads as off, starts
// nothing, and is flagged once so the page can say why; writing the tunnel
// setting clears it, and `quick` itself is no longer accepted.
func TestARetiredQuickTunnelReadsAsOff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "cloudflared")
	ran := fake + ".ran"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch '"+ran+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(map[string]any{"remote": true, "remote_tunnel": "quick", "cloudflared_path": fake})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}}
	t.Cleanup(s.StopTunnel)
	g := s.gate()
	if g.err != nil {
		t.Fatal(g.err)
	}
	if _, _, err := g.auth.AddDevice("phone", auth.NewCaps(auth.Read, auth.Send), false); err != nil {
		t.Fatal(err)
	}
	local, err := g.auth.LocalToken()
	if err != nil {
		t.Fatal(err)
	}
	s.StartTunnel()
	if st := s.tunnel().Status(); st.Phase != "off" || len(st.Command) != 0 {
		t.Fatalf("a stored quick tunnel: %+v", st)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("cloudflared ran for a retired quick tunnel")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/settings", s.settingsRoute)
	h := g.wrap(mux)
	read := func() map[string]any {
		t.Helper()
		rec := call{method: http.MethodGet, path: "/v1/settings", headers: map[string]string{"Authorization": "Bearer " + local}}.do(h)
		var got map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("settings: %d %s", rec.Code, rec.Body)
		}
		return got
	}
	if got := read(); got["remote_tunnel"] != "off" || got["quick_tunnel_retired"] != true {
		t.Fatalf("the settings page reads remote_tunnel %v, retired %v", got["remote_tunnel"], got["quick_tunnel_retired"])
	}
	write := func(body string) int {
		return call{path: "/v1/settings", body: body, headers: map[string]string{
			"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h).Code
	}
	if code := write(`{"remote_tunnel":"quick"}`); code != http.StatusBadRequest {
		t.Fatalf("writing quick answered %d", code)
	}
	if code := write(`{"remote_tunnel":"off"}`); code != http.StatusOK {
		t.Fatalf("writing off answered %d", code)
	}
	if got := read(); got["remote_tunnel"] != "off" || got["quick_tunnel_retired"] != nil {
		t.Fatalf("after the explanation: remote_tunnel %v, retired %v", got["remote_tunnel"], got["quick_tunnel_retired"])
	}
}

// The open health answers two things about the door and nothing about the
// machine: whether this asker is let in, and whether there is a password door.
func TestHealthAnswersTheDoor(t *testing.T) {
	f, _ := newGateFixture(t)
	s := &Server{cfg: config.Config{Dir: "/secret/state/dir", Port: 7757}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.health)
	h := f.g.wrap(mux)
	read := func(headers map[string]string) contract.Health {
		t.Helper()
		rec := call{method: http.MethodGet, path: "/v1/health", headers: headers}.do(h)
		var got contract.Health
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("health: %d %s", rec.Code, rec.Body)
		}
		return got
	}
	if got := read(nil); got.Authed || got.Password {
		t.Fatalf("nobody, no password: %+v", got)
	}
	if got := read(map[string]string{"Cookie": "clawdline-next=not-a-token"}); got.Authed {
		t.Fatalf("a cookie that is not ours: %+v", got)
	}
	if got := read(map[string]string{"Cookie": "clawdline-next=" + f.read}); !got.Authed {
		t.Fatalf("a paired reader: %+v", got)
	}
	if err := f.g.auth.SetPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if got := read(nil); got.Authed || !got.Password {
		t.Fatalf("a password door, and nobody through it: %+v", got)
	}
}

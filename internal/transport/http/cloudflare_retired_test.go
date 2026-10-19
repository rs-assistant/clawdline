package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// A settings file written while the built-in Cloudflare tunnel existed still
// starts the daemon and still opens the settings page: nothing launches the
// cloudflared it names, its hostname is no longer a Host the gate answers to,
// the snapshot flags it once so the page can say why, writing any of its keys
// is refused by name, and dismissing the notice takes the keys out of the file
// and leaves everything else as it was.
func TestARetiredCloudflareTunnelStartsNothing(t *testing.T) {
	for name, stored := range map[string]map[string]any{
		"named": {"remote": true, "remote_tunnel": "named", "remote_tunnel_name": "clawdline",
			"remote_hostname": "box.example.com", "terminal": "tmux"},
		"quick": {"remote": true, "remote_tunnel": "quick", "terminal": "tmux"},
	} {
		t.Run(name, func(t *testing.T) { retiredCloudflareTunnel(t, stored) })
	}
}

func retiredCloudflareTunnel(t *testing.T, stored map[string]any) {
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "cloudflared")
	ran := fake + ".ran"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch '"+ran+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stored["cloudflared_path"] = fake
	settings, _ := json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757}}
	g := s.gate()
	if g.err != nil {
		t.Fatal(g.err)
	}
	_, device, err := g.auth.AddDevice("phone", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	local, err := g.auth.LocalToken()
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/settings", s.settingsRoute)
	mux.HandleFunc("/v1/settings/browser", s.settingsRoute)
	mux.HandleFunc("/v1/health", s.health)
	h := g.wrap(mux)
	// The named tunnel's hostname is somebody else's now, even for a paired
	// device and even with remote access on.
	if rec := (call{method: http.MethodGet, path: "/v1/health", host: "box.example.com", headers: map[string]string{
		"Authorization": "Bearer " + device, "X-Forwarded-Proto": "https"}}).do(h); rec.Code != http.StatusForbidden {
		t.Fatalf("the retired tunnel's hostname answered %d", rec.Code)
	}

	read := func() map[string]any {
		t.Helper()
		rec := call{method: http.MethodGet, path: "/v1/settings", headers: map[string]string{"Authorization": "Bearer " + local}}.do(h)
		var got map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("settings: %d %s", rec.Code, rec.Body)
		}
		return got
	}
	got := read()
	if got["cloudflare_tunnel_retired"] != true {
		t.Fatalf("the settings page was not told: %v", got)
	}
	for _, key := range []string{"remote_tunnel", "remote_hostname", "quick_tunnel_retired"} {
		if _, ok := got[key]; ok {
			t.Fatalf("the snapshot still carries %s: %v", key, got[key])
		}
	}
	write := func(path, body string) (int, string) {
		rec := call{path: path, body: body, headers: map[string]string{
			"Authorization": "Bearer " + local, "Content-Type": "application/json"}}.do(h)
		return rec.Code, rec.Body.String()
	}
	for _, body := range []string{`{"remote_tunnel":"named"}`, `{"remote_tunnel":"off"}`, `{"remote_tunnel":"quick"}`,
		`{"remote_hostname":"box.example.com"}`, `{"remote_tunnel_name":"clawdline"}`,
		`{"cloudflared_path":"/usr/local/bin/cloudflared"}`, `{"remote_tunnel":null}`} {
		code, answer := write("/v1/settings", body)
		if code != http.StatusBadRequest || !strings.Contains(answer, "retired_setting") || !strings.Contains(answer, "Tailscale") {
			t.Errorf("writing %s answered %d %s", body, code, answer)
		}
	}
	if code, answer := write("/v1/settings", `{"cloudflare_tunnel_retired":true}`); code != http.StatusBadRequest {
		t.Errorf("writing the flag true answered %d %s", code, answer)
	}
	if got := read(); got["cloudflare_tunnel_retired"] != true {
		t.Fatalf("a refused write changed the file: %v", got)
	}
	// "Got it", from the console the way a paired browser sends it.
	if code, answer := write("/v1/settings/browser", `{"cloudflare_tunnel_retired":false}`); code != http.StatusOK {
		t.Fatalf("dismissing the notice answered %d %s", code, answer)
	}
	got = read()
	if _, ok := got["cloudflare_tunnel_retired"]; ok || got["remote"] != true || got["terminal"] != "tmux" {
		t.Fatalf("after the notice: %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"remote_tunnel", "remote_tunnel_name", "remote_hostname", "cloudflared_path"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatalf("the file still holds %s: %s", key, raw)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("cloudflared ran for a retired tunnel")
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

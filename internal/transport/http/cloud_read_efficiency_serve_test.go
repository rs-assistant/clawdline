package http

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// Opt-in local transport benchmark. The terminal host and roster are synthetic;
// cloudops admission, the HTTP transcript route, parser and execution store are real.
// No daemon, real terminal, Cloud service or credential is used.
func TestServeCloudReadEfficiency(t *testing.T) {
	ready := os.Getenv("CLAWDLINE_READ_BENCH_READY")
	if ready == "" {
		t.Skip("set CLAWDLINE_READ_BENCH_READY to a task-owned ready file")
	}
	item, path, _ := recordSession(t)
	p := waitingPane(item.ID, "")
	p.s = item
	p.s.Backend = session.BackendTmux
	p.s.State = session.StateIdle
	p.s.PID = os.Getpid()
	// slowPane supplies explicit tmux provenance without calling tmux. Zero delay.
	host := &slowPane{pane: p}
	s := paneServer(t, p)
	s.terminals = []ports.TerminalHost{host}
	s.inventory = app.Inventory{Terminals: s.terminals, Screen: p}
	s.readings = app.NewInventoryReading(s.inventory.Read, 0)
	s.executionMachineID = "mac_a"
	inv := s.inventory.Read(context.Background())
	items := inv.Assistants()
	if len(items) != 1 {
		t.Fatalf("synthetic inventory: %+v", inv)
	}
	gen := s.observeExecutions(context.Background(), inv, items, []ownrecords.Live{liveOf(items[0])})[item.ID]
	if gen == "" {
		t.Fatal("no synthetic execution generation")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var fixtureMu sync.Mutex
	version := 0
	appendTurns := func(n int) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		for range n {
			version++
			_ = json.NewEncoder(f).Encode(map[string]any{"type": "user", "uuid": fmt.Sprintf("bench-%d", version),
				"timestamp": "2026-01-01T00:00:00Z", "message": map[string]any{"role": "user",
					"content": fmt.Sprintf("synthetic turn %04d %s", version, strings.Repeat("x", 256))}})
		}
	}
	appendTurns(240)
	var mu sync.Mutex
	var routes []map[string]any
	revoked := false
	bridge := cloudops.Bridge{MachineID: "mac_a", AdmitExecution: s.AdmitExecutionTarget,
		Receipts: s.store, EnforceSessionReceipts: true,
		AllowCommands: func() bool { return true },
		TranscriptAuthority: func(context.Context, string, ed25519.PublicKey) cloudops.Authority {
			mu.Lock()
			defer mu.Unlock()
			return cloudops.Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
				WriteGateAllows: true, ReadTranscriptAllows: !revoked}
		}}
	bridge.Router = cloud.Router{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{
			Allowed: true, Device: "synthetic-viewer", Caps: auth.NewCaps(auth.Read, auth.Send)}}))
		if r.URL.Path == "/v1/transcript" {
			s.transcriptRoute(w, r)
		} else if id, ok := focusPath(r); ok {
			s.sessionFocusRoute(w, r, id)
		} else {
			s.sessionAction(w, r)
		}
		mu.Lock()
		routes = append(routes, map[string]any{"path": r.URL.Path, "query": r.URL.RawQuery,
			"us": time.Since(started).Microseconds()})
		mu.Unlock()
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var once sync.Once
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Sender string         `json:"sender"`
			Seq    uint64         `json:"seq"`
			Rail   string         `json:"rail"`
			Body   map[string]any `json:"body"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "invalid synthetic request", 400)
			return
		}
		plaintext, _ := json.Marshal(in.Body)
		a := bridge.Handle(r.Context(), cloudops.Command{Channel: in.Rail + "/mac_a", Class: cloudops.ClassCtl,
			Sender: in.Sender, Sequence: in.Seq, Plaintext: plaintext})
		var payload any
		_ = json.Unmarshal(a.Payload, &payload)
		reply(w, map[string]any{"status": a.Status, "code": a.Code, "payload": payload, "payloadBytes": len(a.Payload)})
	})
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		fixtureMu.Lock()
		defer fixtureMu.Unlock()
		if r.Method == "POST" {
			appendTurns(1)
		}
		reply(w, map[string]any{"version": version, "generation": gen, "session": item.ID})
	})
	mux.HandleFunc("/routes", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		reply(w, append([]map[string]any{}, routes...))
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		revoked = r.URL.Query().Get("on") == "1"
		mu.Unlock()
		reply(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"ok": true})
		once.Do(func() { close(stop) })
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	if err := os.WriteFile(ready, []byte("http://"+listener.Addr().String()), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stop:
	case <-time.After(5 * time.Minute):
		t.Error("benchmark did not stop its server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

package http

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// An event stream is one response that can stay open for hours. A device
// revoked while it reads one used to keep receiving every session frame until
// it disconnected on its own.
func TestARevokedDeviceStopsReadingTheEventStream(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_STREAM", "250ms")
	g := openGate(config.Config{Dir: filepath.Join(t.TempDir(), "next"), Port: 7757})
	if g.err != nil {
		t.Fatal(g.err)
	}
	id, token, err := g.auth.AddDevice("phone", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, State: session.StateIdle}}
	s := paneServer(t, p)
	s.screenBus = newScreenBus()
	ts := httptest.NewServer(g.wrap(http.HandlerFunc(s.ownEvents)))
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the stream was refused before the revocation: %d", res.StatusCode)
	}
	frames := make(chan struct{}, 64)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		defer res.Body.Close()
		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 0, 1<<16), 1<<22)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event: sessions") {
				select {
				case frames <- struct{}{}:
				default:
				}
			}
		}
	}()
	select {
	case <-frames:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream sent no first frame")
	}
	select {
	case <-ended:
		t.Fatal("the stream ended before the device was revoked")
	case <-time.After(time.Second):
	}
	if err := g.auth.Revoke(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the revoked device's stream is still open")
	}
}

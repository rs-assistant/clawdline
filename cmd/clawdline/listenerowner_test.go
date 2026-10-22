package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/sockowner/othersock"
)

// The review's finding: another user binds the daemon's port first, and the
// first command that knocks hands it this machine's token. The command must
// refuse before a byte is written, and say whose port it was.
func TestTheCLISendsNoCredentialToAnotherUsersPort(t *testing.T) {
	ln, err := othersock.Listen(othersock.Nobody, 0)
	if errors.Is(err, othersock.ErrUnavailable) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	heard := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			data, _ := io.ReadAll(c)
			_ = c.Close()
			heard <- string(data)
		}
	}()
	// Both shapes every daemon call takes: the default client, and a client
	// of its own with a timeout and no Transport.
	for name, client := range map[string]*http.Client{
		"http.DefaultClient":      http.DefaultClient,
		"a client with a timeout": {Timeout: 5 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/v1/diagnostics", nil)
			req.Header.Set("Authorization", "Bearer the-local-token")
			res, err := client.Do(req)
			if err == nil {
				res.Body.Close()
			}
			select {
			case got := <-heard:
				if strings.Contains(got, "the-local-token") {
					t.Fatalf("the other user's listener read the token:\n%s", got)
				}
				if got != "" {
					t.Fatalf("the other user's listener read %q", got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the listener never saw the connection close")
			}
			if err == nil || !errors.Is(err, errListenerNotOurs) || !strings.Contains(err.Error(), "uid 65534") {
				t.Fatalf("err = %v; want the port named as another user's", err)
			}
		})
	}
}

// This user's own listener is reached as before.
func TestTheCLIReachesItsOwnUsersPort(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("Authorization"))
	}))
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set("Authorization", "Bearer mine")
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if body, _ := io.ReadAll(res.Body); string(body) != "Bearer mine" {
		t.Fatalf("own daemon answered %q", body)
	}
}

// When the kernel's table cannot be read, nothing is sent.
func TestTheCLISendsNothingWhenItCannotTellWhoListens(t *testing.T) {
	old := listenerOwner
	listenerOwner = func(local, remote netip.AddrPort) (uint32, error) {
		return 0, errors.New("open /proc/net/tcp: permission denied")
	}
	defer func() { listenerOwner = old }()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the request reached the listener")
	}))
	defer ts.Close()
	res, err := http.Get(ts.URL)
	if err == nil {
		res.Body.Close()
		t.Fatal("a request went out to a listener nobody could identify")
	}
	if !strings.Contains(err.Error(), "could not tell which user") {
		t.Fatalf("err = %v", err)
	}
}

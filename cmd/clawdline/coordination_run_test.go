package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCoordinatedLandingRunsOnlyAfterGrantAndReleases(t *testing.T) {
	var moves []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moves = append(moves, r.URL.Path)
		if r.URL.Path == "/v1/orchestrator/leases" {
			fmt.Fprint(w, `{"state":"granted"}`)
		} else {
			fmt.Fprint(w, `{"state":"released"}`)
		}
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "test", client: server.Client()}
	var stdout, stderr bytes.Buffer
	ran := false
	code := runCoordinatedOperation(&stdout, &stderr, b, coordinationRunOptions{resource: "landing", checkout: t.TempDir(), maxWait: time.Minute},
		[]string{"git", "merge"}, time.Now, time.Sleep,
		func(key string) string {
			if key == "CODEX_THREAD_ID" {
				return "379d0000-0000-4000-8000-000000000001"
			}
			return ""
		},
		func(argv, env []string) (int, error) {
			ran = true
			if len(env) != 0 {
				t.Fatalf("landing unexpectedly changed child environment: %v", env)
			}
			if len(moves) != 1 {
				t.Fatalf("command ran before grant: %v", moves)
			}
			return 0, nil
		})
	if code != 0 || !ran || len(moves) != 2 || moves[1] != "/v1/orchestrator/leases/release" {
		t.Fatalf("code %d, ran %v, moves %v, stderr %q", code, ran, moves, stderr.String())
	}
}

func TestRestartWrapperMarksChildSoHeavyRefusesBeforeQueuing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/orchestrator/leases" {
			fmt.Fprint(w, `{"state":"granted"}`)
		} else {
			fmt.Fprint(w, `{"state":"released"}`)
		}
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "test", client: server.Client()}
	var stdout, stderr bytes.Buffer
	code := runCoordinatedOperation(&stdout, &stderr, b, coordinationRunOptions{resource: "daemon_restart", maxWait: time.Minute},
		[]string{"clawdline", "heavy", "--", "go", "build"}, time.Now, time.Sleep,
		func(key string) string {
			if key == "CODEX_THREAD_ID" {
				return "379d0000-0000-4000-8000-000000000001"
			}
			return ""
		},
		func(argv, env []string) (int, error) {
			if len(env) != 1 || env[0] != "CLAWDLINE_DAEMON_RESTART_HELD=1" {
				t.Fatalf("child env: %v", env)
			}
			deps := heavyDeps{getenv: func(key string) string {
				if key == "CLAWDLINE_DAEMON_RESTART_HELD" {
					return "1"
				}
				return ""
			}, stderr: &stderr,
				run: func([]string, []string) (int, error) { t.Fatal("heavy command ran"); return 0, nil }}
			return runHeavy(heavyOptions{}, []string{"go", "build"}, deps), nil
		})
	if code != 2 || !bytes.Contains(stderr.Bytes(), []byte("daemon_restart")) {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
}

func TestCoordinatedRestartNeverRunsAfterQueueDeadline(t *testing.T) {
	var moves []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moves = append(moves, r.URL.Path)
		fmt.Fprint(w, `{"state":"queued","position":1}`)
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "test", client: server.Client()}
	var stdout, stderr bytes.Buffer
	clock := time.Unix(0, 0)
	code := runCoordinatedOperation(&stdout, &stderr, b, coordinationRunOptions{resource: "daemon_restart", maxWait: time.Second},
		[]string{"restart"}, func() time.Time { return clock }, func(d time.Duration) { clock = clock.Add(d) },
		func(key string) string {
			if key == "CODEX_THREAD_ID" {
				return "379d0000-0000-4000-8000-000000000001"
			}
			return ""
		},
		func([]string, []string) (int, error) { t.Fatal("ran without a grant"); return 0, nil })
	if code != heavyExitTimedOut || len(moves) != 3 || moves[2] != "/v1/orchestrator/leases/cancel" {
		t.Fatalf("code %d, moves %v, stderr %q", code, moves, stderr.String())
	}
}

// A lease the daemon says is lost while the command runs is said, the way
// heavy says it, instead of being renewed into silence.
func TestCoordinatedRunSaysALostLease(t *testing.T) {
	renewed := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/orchestrator/leases":
			fmt.Fprint(w, `{"state":"granted"}`)
		case "/v1/orchestrator/leases/renew":
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"ok":false,"error":{"code":"lease_lost","message":"the lease is no longer held"}}`)
			renewed <- struct{}{}
		default:
			fmt.Fprint(w, `{"state":"released"}`)
		}
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "test", client: server.Client()}
	var stdout, stderr bytes.Buffer
	code := runCoordinatedOperation(&stdout, &stderr, b,
		coordinationRunOptions{resource: "landing", checkout: t.TempDir(), maxWait: time.Minute, renew: 5 * time.Millisecond},
		[]string{"git", "merge"}, time.Now, time.Sleep,
		func(key string) string {
			if key == "CODEX_THREAD_ID" {
				return "379d0000-0000-4000-8000-000000000002"
			}
			return ""
		},
		func(argv, env []string) (int, error) {
			<-renewed
			time.Sleep(20 * time.Millisecond)
			return 0, nil
		})
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "lease lapsed") {
		t.Fatalf("a lost lease went unsaid: %q", stderr.String())
	}
	if n := len(renewed); n != 0 {
		t.Fatalf("renewed %d more times after the lease was lost", n)
	}
}

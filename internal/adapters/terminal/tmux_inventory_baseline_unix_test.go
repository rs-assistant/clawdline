//go:build darwin || linux

package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestTmuxInventoryBaseline is opt-in: it owns only synthetic sleep panes on an
// absolute private socket. CPU is the Go process alone, excluding tmux clients
// and server. The tiny env wrapper is included in wall time.
func TestTmuxInventoryBaseline(t *testing.T) {
	if os.Getenv("CLAWDLINE_TERMINAL_BASELINE") != "1" {
		t.Skip("opt-in local baseline")
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable; no measurement")
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{1, 4, 16} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "clawdline-inventory-baseline-")
			if err != nil {
				t.Fatal(err)
			}
			sock := filepath.Join(dir, "private.sock")
			// Inventory itself must also invoke env -u TMUX, even if the parent
			// was launched from a live terminal Session.
			wrapper := filepath.Join(dir, "tmux-env")
			if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec /usr/bin/env -u TMUX -u TMUX_PANE '"+strings.ReplaceAll(bin, "'", "'\\''")+"' \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			run := func(args ...string) string {
				t.Helper()
				out, err := exec.Command(wrapper, append([]string{"-S", sock}, args...)...).CombinedOutput()
				if err != nil {
					t.Fatalf("private tmux %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			run("-f", "/dev/null", "new-session", "-d", "-s", "baseline", "-c", dir, "sleep", "300")
			pid := run("display-message", "-p", "#{pid}")
			serverPID, err := strconv.Atoi(pid)
			if err != nil || serverPID <= 1 {
				t.Fatalf("invalid owned server PID %q", pid)
			}
			stopped := false
			defer func() {
				if !stopped {
					_ = exec.Command(wrapper, "-S", sock, "kill-server").Run()
				}
				deadline := time.Now().Add(3 * time.Second)
				for syscall.Kill(serverPID, 0) != syscall.ESRCH {
					if time.Now().After(deadline) {
						t.Errorf("owned tmux PID %d has not exited; retained %s", serverPID, dir)
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				// tmux can briefly leave its socket after kill-server. A successful
				// has-session here means ownership cleanup failed: keep evidence.
				if exec.Command(wrapper, "-S", sock, "has-session").Run() == nil {
					t.Errorf("private server still alive; retained %s", dir)
					return
				}
				if err := os.RemoveAll(dir); err != nil {
					t.Error(err)
				}
				_, e := os.Stat(dir)
				if !os.IsNotExist(e) {
					t.Errorf("socket directory remains: %v", e)
				}
				t.Logf("CLEANUP socket=%s pid=%s pid_absent=true has_session=false directory_absent=%t", sock, pid, os.IsNotExist(e))
			}()
			ids := map[string]bool{}
			ids[run("display-message", "-p", "#{pane_id}")] = true
			for i := 1; i < count; i++ {
				ids[run("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "baseline", "-c", dir, "sleep", "300")] = true
			}
			adapter := &Tmux{Binary: wrapper, Socket: sock}
			resolved, _ := filepath.EvalSymlinks(dir)
			measure := func(phase string) {
				t.Helper()
				u, s := tmuxBaselineCPU(t)
				start := time.Now()
				inv, err := adapter.Inventory(context.Background())
				wall := time.Since(start).Nanoseconds()
				u1, s1 := tmuxBaselineCPU(t)
				if err != nil || !inv.Complete || len(inv.Sessions) != len(ids) {
					t.Fatalf("phase=%s err=%v inventory=%+v expected=%d", phase, err, inv, len(ids))
				}
				seen := map[string]bool{}
				for _, p := range inv.Sessions {
					if !ids[p.ID] || seen[p.ID] || (p.CWD != dir && p.CWD != resolved) {
						t.Fatalf("unexpected pane %+v", p)
					}
					seen[p.ID] = true
				}
				t.Logf("BASELINE tmux panes=%d phase=%s wall_ns=%d go_user_ns=%d go_system_ns=%d", len(ids), phase, wall, u1-u, s1-s)
			}
			measure("positive")
			for i := 0; i < 5; i++ {
				measure("unchanged")
			}
			added := run("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "baseline", "-c", dir, "sleep", "300")
			ids[added] = true
			measure("added")
			run("kill-pane", "-t", added)
			delete(ids, added)
			measure("removed")
			run("kill-server")
			stopped = true
			inv, err := adapter.Inventory(context.Background())
			if err != nil || !inv.Complete || len(inv.Sessions) != 0 {
				t.Fatalf("stopped server: %+v %v", inv, err)
			}
			wrong := &Tmux{Binary: wrapper, Socket: filepath.Join(dir, "absent.sock")}
			inv, err = wrong.Inventory(context.Background())
			if err != nil || !inv.Complete || len(inv.Sessions) != 0 {
				t.Fatalf("absent socket: %+v %v", inv, err)
			}
		})
	}
}

func tmuxBaselineCPU(t *testing.T) (int64, int64) {
	t.Helper()
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		t.Fatal(err)
	}
	return r.Utime.Nano(), r.Stime.Nano()
}

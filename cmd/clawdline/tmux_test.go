package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTmuxKillServerRefusalNeverCallsTmux(t *testing.T) {
	if os.Getenv("CLAWDLINE_TMUX_GUARD_HELPER") == "1" {
		tmuxCommand([]string{"kill-server"})
		return
	}
	if os.PathSeparator == '\\' {
		t.Skip("shell fixture requires Unix")
	}
	dir, err := os.MkdirTemp("/tmp", "cltg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	marker := filepath.Join(dir, "called")
	fake := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nif [ \"$1\" = -V ]; then echo 'tmux 3.6a'; exit 0; fi\nprintf called > " + marker + "\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTmuxKillServerRefusalNeverCallsTmux$")
	cmd.Env = append(os.Environ(), "CLAWDLINE_TMUX_GUARD_HELPER=1", "PATH="+dir, "TMUX="+filepath.Join(dir, "active.sock")+",123,0", "TMUX_TMPDIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "-S <private absolute socket>") {
		t.Fatalf("refusal = %v: %s", err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("tmux was invoked: %v", err)
	}
}

func TestTmuxDestructionRequiresPrivateSocket(t *testing.T) {
	active := filepath.Join(t.TempDir(), "main.sock")
	private := filepath.Join(t.TempDir(), "private.sock")
	for _, args := range [][]string{{"kill-server"}, {"kill-session", "-a"}, {"-L", "test", "kill-server"}, {"-S", active, "kill-server"}} {
		if err := guardTmuxDestruction(args, "", active+",123,0"); err == nil || !strings.Contains(err.Error(), "-S <private") {
			t.Fatalf("%q: expected private socket refusal, got %v", args, err)
		}
	}
	if err := guardTmuxDestruction([]string{"-S", private, "kill-server"}, "", active+",123,0"); err != nil {
		t.Fatal(err)
	}
	if err := guardTmuxDestruction([]string{"-S", private, "kill-server"}, private, ""); err == nil {
		t.Fatal("daemon socket accepted")
	}
}

func TestPrivateTmuxCanBeKilledWithoutTouchingActiveServer(t *testing.T) {
	if socket := os.Getenv("CLAWDLINE_TMUX_PRIVATE_SOCK"); socket != "" {
		tmuxCommand([]string{"-S", socket, "kill-server"})
		return
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("/tmp", "cltm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	mainSock := filepath.Join(dir, "main.sock")
	privateSock := filepath.Join(dir, "private.sock")
	start := func(sock string) {
		t.Helper()
		cmd := exec.Command(bin, "-S", sock, "new-session", "-d", "-s", "sentinel", "sleep", "60")
		cmd.Env = append(os.Environ(), "TMUX=")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("start %s: %v: %s", sock, err, out)
		}
	}
	start(mainSock)
	t.Cleanup(func() { _ = exec.Command(bin, "-S", mainSock, "kill-server").Run() })
	start(privateSock)
	t.Cleanup(func() { _ = exec.Command(bin, "-S", privateSock, "kill-server").Run() })
	if err := guardTmuxDestruction([]string{"kill-server"}, "", mainSock+",123,0"); err == nil {
		t.Fatal("unguarded kill-server")
	}
	if err := exec.Command(bin, "-S", mainSock, "has-session", "-t", "sentinel").Run(); err != nil {
		t.Fatal("active server lost after refusal:", err)
	}
	if err := guardTmuxDestruction([]string{"-S", privateSock, "kill-server"}, "", mainSock+",123,0"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPrivateTmuxCanBeKilledWithoutTouchingActiveServer$")
	cmd.Env = append(os.Environ(), "CLAWDLINE_TMUX_PRIVATE_SOCK="+privateSock, "TMUX="+mainSock+",123,0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clawdline private kill: %v: %s", err, out)
	}
	if err := exec.Command(bin, "-S", mainSock, "has-session", "-t", "sentinel").Run(); err != nil {
		t.Fatal("active server lost after private kill:", err)
	}
}

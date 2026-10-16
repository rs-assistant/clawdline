package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
)

// tmuxCommand is `clawdline tmux <args…>`: the tmux this daemon runs, on the
// server it runs it on (terminal.ResolveTmux). On a machine with its own tmux
// that is just that tmux; on one without, it is the tmux the release carries,
// pointed at Clawdline's own socket — so `clawdline tmux new -s work` and
// `clawdline tmux attach` reach the sessions the daemon lists, where a bare
// `tmux` would not exist at all.
//
// `clawdline tmux which` says which one, and from where, instead of running it.
func tmuxCommand(args []string) {
	choice := terminal.ResolveTmux(context.Background())
	if len(args) == 1 && args[0] == "which" {
		fmt.Println(choice.Describe())
		if choice.Passed != "" {
			fmt.Printf(cliCopy("misc", "tmux.passed", "  (%s)\n"), choice.Passed)
		}
		if !choice.Found() {
			os.Exit(1)
		}
		return
	}
	if !choice.Found() {
		fmt.Fprintln(os.Stderr, cliCopy("misc", "tmux.none", "clawdline tmux: there is no tmux on this machine, and this build carries none"))
		os.Exit(1)
	}
	if err := guardTmuxDestruction(args, choice.Socket, os.Getenv("TMUX")); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline tmux:", tmuxGuardCopy(err))
		os.Exit(2)
	}
	cmd := exec.Command(choice.Path, choice.Args(args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "clawdline tmux: %v\n", err)
		os.Exit(1)
	}
}

func tmuxGuardCopy(err error) string {
	if strings.Contains(err.Error(), "requires -S") {
		return fmt.Sprintf(cliCopy("misc", "tmux.private_socket_required", "%s requires -S <private absolute socket>; TMUX_TMPDIR does not override $TMUX"), strings.Fields(err.Error())[0])
	}
	if strings.Contains(err.Error(), "daemon's tmux server") {
		return fmt.Sprintf(cliCopy("misc", "tmux.daemon_socket_refused", "%s targets the daemon's tmux server; use -S <private socket>"), strings.Fields(err.Error())[0])
	}
	if strings.Contains(err.Error(), "current tmux server") {
		return fmt.Sprintf(cliCopy("misc", "tmux.current_socket_refused", "%s targets the current tmux server; use -S <private socket>"), strings.Fields(err.Error())[0])
	}
	return err.Error()
}

// Destructive server-wide commands require an absolute private socket. TMUX
// takes precedence over TMUX_TMPDIR, even when the latter names a test server.
func guardTmuxDestruction(args []string, daemonSocket, tmuxEnv string) error {
	var socket, command string
	commandAt := -1
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-S":
			if i+1 >= len(args) {
				return fmt.Errorf("-S needs a socket path")
			}
			i++
			socket = args[i]
		case "-L":
			if i+1 >= len(args) {
				return fmt.Errorf("-L needs a socket name")
			}
			i++
		case "kill-server", "kill-session":
			command = a
			commandAt = i
		}
	}
	destructive := command == "kill-server"
	if command == "kill-session" {
		for _, a := range args[commandAt+1:] {
			if a == "-a" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a[1:], "a")) {
				destructive = true
			}
		}
	}
	if !destructive {
		return nil
	}
	if !filepath.IsAbs(socket) {
		return fmt.Errorf("%s requires -S <private absolute socket>; TMUX_TMPDIR does not override $TMUX", command)
	}
	if daemonSocket != "" && sameTmuxSocket(socket, daemonSocket) {
		return fmt.Errorf("%s targets the daemon's tmux server; use -S <private socket>", command)
	}
	if active, _, ok := strings.Cut(tmuxEnv, ","); ok && sameTmuxSocket(socket, active) {
		return fmt.Errorf("%s targets the current tmux server; use -S <private socket>", command)
	}
	return nil
}

func sameTmuxSocket(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if real, err := filepath.EvalSymlinks(a); err == nil {
		a = real
	}
	if real, err := filepath.EvalSymlinks(b); err == nil {
		b = real
	}
	return a == b
}

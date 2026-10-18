//go:build !windows

package owned

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// A shell that ends by itself takes its terminal with it (`remain-on-exit
// off`), and tmux keeps nothing of how it ended: measured on 3.6a, a
// `pane-exited` hook runs after the pane is gone, so `#{pane_dead_status}`
// and `#{pane_dead_signal}` there are empty for `exit 3` and `kill -9`
// alike, and `#{session_name}` is some other session's. Only the shell's
// parent learns its status, so the pane runs the shell under a small
// `/bin/sh` that waits for it and writes the status to a file named by the
// terminal's id; the daemon logs that file once and removes it.
//
// The wrapper writes nothing when it is itself hung up, which is what
// `kill-session` does, so a terminal the daemon closed leaves no line.
// Without job control a Ctrl-C would reach it too; a login shell at a
// prompt has job control and puts itself in its own process group, so the
// wrapper never sees the keyboard's signals (measured: zsh at a prompt,
// `sleep 100` and Ctrl-C, the wrapper still waiting).
//
// The id and the path come in as arguments, never into the script's text,
// so nothing in them is read by a shell.
const endWrapper = `f=$1; shift; "$@"; s=$?; printf '%s\n' "$s" > "$f" 2>/dev/null; exit "$s"`

// endsDir holds one file per shell that ended by itself and has not been
// logged yet.
func (s *Server) endsDir() string { return filepath.Join(filepath.Dir(s.sock), "ends") }

func (s *Server) endPath(id terminal.ID) string { return filepath.Join(s.endsDir(), string(id)) }

// logEnds writes one daemon log line for every shell that ended by itself
// since it last looked: the terminal's id, when, and the exit status. A
// status above 128 is how a shell reports a signal, so the signal is named
// beside it; a shell that ran `exit 137` reads the same, and the line says
// "as" rather than "by". Only the status is read: nothing typed and nothing
// on the screen reaches the file.
//
// The file is removed before its line is written, so two readers at once
// cannot both log it.
func (s *Server) logEnds() {
	if s.logf == nil {
		return
	}
	entries, err := os.ReadDir(s.endsDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		id := terminal.ID(e.Name())
		if !id.Valid() {
			continue
		}
		path := s.endPath(id)
		info, ierr := e.Info()
		data, rerr := os.ReadFile(path)
		if err := os.Remove(path); err != nil {
			continue
		}
		at := "at an unknown time"
		if ierr == nil {
			at = "at " + info.ModTime().UTC().Format(time.RFC3339)
		}
		s.logf("terminals: %s ended by itself %s: %s", id, at, endStatus(data, rerr))
	}
}

// endStatus is the wrapper's record as words.
func endStatus(data []byte, err error) string {
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "its exit status could not be read: " + err.Error()
	}
	status, perr := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || perr != nil || status < 0 {
		return "its exit status could not be read"
	}
	if status > 128 && status < 128+65 {
		sig := status - 128
		return "its shell exited with status " + strconv.Itoa(status) + ", as signal " + strconv.Itoa(sig) +
			" (" + syscall.Signal(sig).String() + ") would leave it"
	}
	return "its shell exited with status " + strconv.Itoa(status)
}

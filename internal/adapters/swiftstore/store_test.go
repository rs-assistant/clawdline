package swiftstore

import (
	"testing"
	"time"
)

// A record is about this process only when all six facts agree; the start
// time within the Swift app's five seconds, and the tty in either spelling.
func TestARecordMatchesOnlyItsOwnProcess(t *testing.T) {
	claude, conv := "claude", "c"
	pid := int64(42)
	start := Seconds(1000.4)
	rec := Identity{TerminalID: "%9", TTY: "/dev/ttys009", Assistant: &claude, PID: &pid, ProcessStart: &start, ConversationID: &conv}
	live := Live{TerminalID: "%9", TTY: "ttys009", Assistant: "claude", PID: 42, ProcessStart: time.Unix(1004, 0), ConversationID: "c"}
	if !rec.Matches(live) {
		t.Fatal("same process did not match")
	}
	for name, change := range map[string]func(*Live){
		"pid":          func(l *Live) { l.PID = 43 },
		"start":        func(l *Live) { l.ProcessStart = time.Unix(1010, 0) },
		"conversation": func(l *Live) { l.ConversationID = "d" },
		"terminal":     func(l *Live) { l.TerminalID = "%10" },
		"unreadable":   func(l *Live) { l.ProcessStart = time.Time{} },
	} {
		l := live
		change(&l)
		if rec.Matches(l) {
			t.Errorf("a different %s still matched", name)
		}
	}
}

package swiftstore

import "sort"

// SessionTitle is a name typed in Clawdline (or, when Automatic, one a model
// chose for a Claude conversation that had none yet).
type SessionTitle struct {
	Title              string   `json:"title"`
	TerminalID         string   `json:"terminal_id"`
	SessionID          *string  `json:"session_id"`
	Automatic          bool     `json:"automatic"`
	StartedAt          *Seconds `json:"started_at"`
	SeenCustomTitle    *string  `json:"seen_custom_title"`
	SeenTranscriptPath *string  `json:"seen_transcript_path"`
	UpdatedAt          *Seconds `json:"updated_at"`
}

// Snapshot is the record set the session projections read: this daemon's own
// records, in the shape the retired Swift app wrote them (own.go), so that the
// rules restated in project.go read them unchanged. The retired app's own
// store is no longer read (docs/cutover.md §7); a Snapshot starts empty and is
// filled by With.
type Snapshot struct {
	Orchestrator

	// Coordinator is the registered Clawdfather, or nil when there is none.
	Coordinator *Coordinator

	// Titles are the names a person gave sessions, in order (the last one
	// that matches wins, as in Config.swift). TitlesKnown says they were read.
	Titles      []SessionTitle
	TitlesKnown bool

	// BoardTitles are this daemon's own: the Board label a conversation was
	// opened with, by conversation (own.go). The Swift store holds none.
	BoardTitles []BoardTitle
}

// tasksOldestFirst is the order the closeability index walks them in.
func tasksOldestFirst(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}

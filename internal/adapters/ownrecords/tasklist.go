package ownrecords

import (
	"sort"

	"github.com/sainteye/clawdline/internal/contract"
)

// OnScreen is one session on screen, for resolving a task's root terminal.
type OnScreen struct {
	TerminalID     string
	Assistant      string
	ConversationID string
}

// TaskPage is OrchestratorTaskList.body over this daemon's own tasks: every
// unfinished task, then the page of finished ones starting at cursor, newest
// first. The retired Swift app's tasks are no longer read (docs/cutover.md §7).
func TaskPage(rows []contract.TaskRow, cursor, limit int) ([]contract.TaskRow, contract.TaskPage) {
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	all := append([]contract.TaskRow(nil), rows...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Created > all[j].Created })

	live := []contract.TaskRow{}
	finished := []contract.TaskRow{}
	for _, r := range all {
		if isTerminalState(string(r.State)) {
			finished = append(finished, r)
		} else {
			live = append(live, r)
		}
	}
	start := cursor
	if start > len(finished) {
		start = len(finished)
	}
	end := start + limit
	if end > len(finished) {
		end = len(finished)
	}
	page := contract.TaskPage{
		Cursor:     int64(start),
		Limit:      int64(limit),
		Fields:     "list",
		Unfinished: int64(len(live)),
		Finished:   int64(len(finished)),
	}
	if end < len(finished) {
		page.NextCursor = int64(end)
	}
	return append(live, finished[start:end]...), page
}

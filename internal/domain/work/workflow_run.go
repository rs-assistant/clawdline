package work

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The console starts a Workflow by writing its launch message as a Session
// to-do and appending this marker (web/console/src/session/workflow-run.ts).
// The daemon reads the same marker to keep one Workflow running per Session.
const (
	workflowRunMarker = "\n\n[Clawdline-Workflow "
	workflowRunEnd    = "]"
)

var workflowRunIDPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// WorkflowRunTitle is the title of the Workflow a to-do text starts, and
// false when the text is an ordinary to-do. It accepts exactly what the
// console's workflowRunOf accepts.
func WorkflowRunTitle(text string) (string, bool) {
	at := strings.LastIndex(text, workflowRunMarker)
	if at < 0 || !strings.HasSuffix(text, workflowRunEnd) || at+len(workflowRunMarker) > len(text)-len(workflowRunEnd) {
		return "", false
	}
	data := text[at+len(workflowRunMarker) : len(text)-len(workflowRunEnd)]
	split := strings.IndexByte(data, ' ')
	if split < 1 || !workflowRunIDPattern.MatchString(data[:split]) {
		return "", false
	}
	// decodeURIComponent also refuses escapes that do not spell UTF-8.
	title, err := url.PathUnescape(data[split+1:])
	if err != nil || title == "" || !utf8.ValidString(title) {
		return "", false
	}
	return title, true
}

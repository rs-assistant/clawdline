package http

import (
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"testing"
	"time"
)

func TestCallbackRowsDistinguishQueueExecutionAndCompletion(t *testing.T) {
	r := orchestrator.Record{ID: "task", Title: "Build the dev", CreatedAt: time.Unix(10, 0), State: orchestrator.StateBriefed, Callback: &orchestrator.Callback{Argv: []string{"go", "test", "./..."}, Dir: "/repo", StartedAt: time.Unix(12, 0)}}
	if got := callbackShell(r, nil); got.State != "running" || got.Kind != "callback" || got.Command != "go test ./..." || got.StartedAt != 12 {
		t.Fatalf("%+v", got)
	}
	if got := callbackShell(r, map[string]heavySlotPlace{"task": {phase: "queued"}}); got.State != "queued" {
		t.Fatalf("%+v", got)
	}
	r.State = orchestrator.StateFailure
	if got := callbackShell(r, nil); got.State != "failure" {
		t.Fatalf("%+v", got)
	}
}

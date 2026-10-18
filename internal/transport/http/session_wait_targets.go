package http

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// MaximumWaitTargets is how many waited-on tasks one Session row names. A
// row past it names the first ones in store order and counts the rest in
// `waiting_for_omitted`, so a root with many children stays one line.
const MaximumWaitTargets = 8

// waitTargetsSeen is the most waited-on tasks one row has held since this
// daemon started, before the bound: the capacity row's measurement.
var waitTargetsSeen atomic.Int64

func waitTargetsReading() capacity.Reading {
	return capacity.Reading{Known: true, Used: waitTargetsSeen.Load()}
}

// waitTargets says what a broker-decided waiting_session waits for, from the
// very tasks the work projection counted. A dispatched child is named by its
// Session; a heavy callback by the command it runs and its place at the
// compile slot; anything else by its own title.
func (s *Server) waitTargets(ctx context.Context, tasks []*ownrecords.Task, labels map[string]string,
	places map[string]heavySlotPlace) ([]contract.SessionWaitTarget, int64) {
	if n := int64(len(tasks)); n > waitTargetsSeen.Load() {
		waitTargetsSeen.Store(n)
	}
	omitted := int64(0)
	if len(tasks) > MaximumWaitTargets {
		omitted = int64(len(tasks) - MaximumWaitTargets)
		tasks = tasks[:MaximumWaitTargets]
	}
	var out []contract.SessionWaitTarget
	for _, t := range tasks {
		target := contract.SessionWaitTarget{TaskID: t.ID, Kind: "task", Label: t.Title}
		switch {
		case t.CallbackIntent == orchestrator.CallbackIntentHeavy:
			target.Kind = "heavy"
			if place, ok := places[t.ID]; ok && place.reason != "" {
				target.Label, target.Phase = place.reason, place.phase
			} else if reason := s.heavyCallbackReason(ctx, t.ID); reason != "" {
				target.Label = reason
			}
		case t.CallbackIntent != "":
			target.Kind = "callback"
		default:
			name := ""
			if t.ChildTerminal != nil {
				name = labels[*t.ChildTerminal]
			}
			if name == "" && t.ChildSession != nil {
				name = *t.ChildSession
			}
			if name != "" {
				target.Kind, target.Label = "session", name
			}
		}
		if target.Label == "" {
			target.Label = t.ID
		}
		out = append(out, target)
	}
	return out, omitted
}

// heavyCallbackReason reads a heavy callback's command from its own record
// when it is not at the compile slot: still starting, or finished and being
// delivered. It is what `clawdline heavy` would declare: its --reason, or
// else the command after the separator.
func (s *Server) heavyCallbackReason(ctx context.Context, id string) string {
	if s.broker == nil {
		return ""
	}
	record, _, err := s.broker.Record(ctx, id)
	if err != nil || record.Callback == nil {
		return ""
	}
	return heavyArgvReason(record.Callback.Argv)
}

func heavyArgvReason(argv []string) string {
	for i, arg := range argv {
		switch {
		case arg == "--reason" && i+1 < len(argv):
			return argv[i+1]
		case strings.HasPrefix(arg, "--reason="):
			return strings.TrimPrefix(arg, "--reason=")
		case arg == "--":
			return strings.Join(argv[i+1:], " ")
		}
	}
	return ""
}

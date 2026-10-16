package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// runningHeavyCallback returns the one callback that both declared heavy work
// and presently owns the machine's compile slot. A queued callback, a callback
// waiting for another result, and a direct heavy command are not this marker.
//
// The same one lease reading also answers, for every heavy callback that is in
// the compile slot or in line for it, what it runs and where it stands, which
// is how a Session waiting on one says what it waits for.
func (s *Server) runningHeavyCallback(ctx context.Context) (string, *contract.SessionHeavyWork, map[string]heavySlotPlace) {
	if s.broker == nil {
		return "", nil, nil
	}
	leases, err := s.broker.Leases(ctx)
	if err != nil {
		return "", nil, nil
	}
	for _, lease := range leases {
		if lease.Resource != orchestrator.ResourceCompile {
			continue
		}
		places := heavySlotPlaces(lease)
		holder := lease.Holder
		if holder == nil || holder.CallbackTaskID == "" || holder.Session == "" {
			return "", nil, places
		}
		record, _, err := s.broker.Record(ctx, holder.CallbackTaskID)
		if err != nil {
			return "", nil, places
		}
		id, work := heavyCallbackOwner(holder, record)
		return id, work, places
	}
	return "", nil, nil
}

// heavySlotPlace is a callback's place at the compile slot: holding it, or in
// line for it, with the reason its heavy command declared.
type heavySlotPlace struct {
	reason string
	phase  string
}

func heavySlotPlaces(lease orchestrator.LeaseView) map[string]heavySlotPlace {
	places := map[string]heavySlotPlace{}
	if h := lease.Holder; h != nil && h.CallbackTaskID != "" {
		// A holder still waiting for memory has not started its command.
		phase := "queued"
		if h.Phase == "running" {
			phase = "running"
		}
		places[h.CallbackTaskID] = heavySlotPlace{reason: h.Reason, phase: phase}
	}
	for _, w := range lease.Queue {
		if w.CallbackTaskID != "" {
			places[w.CallbackTaskID] = heavySlotPlace{reason: w.Reason, phase: "queued"}
		}
	}
	return places
}

func heavyCallbackOwner(holder *orchestrator.LeaseHolder, record orchestrator.Record) (string, *contract.SessionHeavyWork) {
	if holder == nil || holder.CallbackTaskID == "" || holder.Session == "" || holder.Phase != "running" || holder.Liveness != "alive" ||
		record.ID != holder.CallbackTaskID || record.Callback == nil || record.Callback.Intent != orchestrator.CallbackIntentHeavy ||
		record.Root == nil || record.Root.SessionID != holder.Session ||
		(record.State != orchestrator.StateQueued && record.State != orchestrator.StateSpawning && record.State != orchestrator.StateBriefed) {
		return "", nil
	}
	return holder.Session, &contract.SessionHeavyWork{TaskID: record.ID, Reason: holder.Reason, StartedAt: holder.AcquiredAt.Unix()}
}

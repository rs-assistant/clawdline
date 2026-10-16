package agenthandoff

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	contract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

const taskStartReceiptScope = "cloud.remote_task_start"

// TaskStartResult distinguishes an opened Session from a claim whose effect
// may have run but cannot be proved. A target never starts again on an
// orphaned claim.
type TaskStartResult struct {
	TaskID           string `json:"task_id"`
	RequestID        string `json:"request_id"`
	Code             string `json:"code"`
	MachineExecution string `json:"machine_execution"`
	TargetSessionID  string `json:"target_session_id,omitempty"`
	TargetGeneration string `json:"target_generation,omitempty"`
}

// TaskStarter accepts a machine-targeted start after both sides' authority
// has been checked. Open is responsible for persisting the task-to-Session
// association atomically with the local opening workflow.
type TaskStarter struct {
	Receipts Receipts
	Facts    func(context.Context, contract.TaskStart, contract.Principal) (contract.TaskStartFacts, error)
	Open     func(context.Context, contract.TaskStart, []byte) (sessionID, generation string, err error)
	Now      func() time.Time
}

func (s TaskStarter) Start(ctx context.Context, start contract.TaskStart, body []byte,
	principal contract.Principal) TaskStartResult {
	answer := TaskStartResult{TaskID: start.TaskID, RequestID: start.RequestID,
		Code: "handoff_unavailable", MachineExecution: "unknown"}
	if s.Receipts == nil || s.Facts == nil || s.Open == nil {
		return answer
	}
	read := func() error {
		facts, err := s.Facts(ctx, start, principal)
		if err != nil {
			return contract.GrantUnavailable
		}
		facts.Principal = principal
		return contract.AdmitTaskStart(start, body, facts)
	}
	if err := read(); err != nil {
		answer.Code, answer.MachineExecution = err.Error(), "rejected"
		return answer
	}
	actor, key, digest := contract.TaskStartReceiptIdentity(start)
	receiptKey := store.ReceiptKey{Scope: taskStartReceiptScope, Actor: actor, Key: key}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	claim, err := s.Receipts.ClaimReceipt(ctx, receiptKey, digest, store.ReceiptPolicy{}, now)
	if err != nil {
		answer.Code = "handoff_receipt_unavailable"
		return answer
	}
	switch claim.Outcome {
	case store.ReceiptMismatch:
		answer.Code, answer.MachineExecution = "handoff_request_id_reused", "rejected"
	case store.ReceiptExpired, store.ReceiptOrphaned:
		answer.Code = "handoff_outcome_unknown"
	case store.ReceiptPending:
		answer.Code, answer.MachineExecution = "handoff_receipt_pending", "pending"
	case store.ReceiptFull:
		answer.Code, answer.MachineExecution = "handoff_receipt_capacity", "rejected"
	case store.ReceiptReplay:
		var previous TaskStartResult
		if json.Unmarshal(claim.Answer.Body, &previous) == nil &&
			previous.TaskID == start.TaskID && previous.RequestID == start.RequestID {
			return previous
		}
		answer.Code = "handoff_receipt_unavailable"
	case store.ReceiptNew:
		if err := read(); err != nil {
			answer.Code, answer.MachineExecution = err.Error(), "rejected"
		} else if id, generation, err := s.Open(ctx, start, body); err == nil && id != "" && generation != "" {
			answer.Code, answer.MachineExecution = "ok", "completed"
			answer.TargetSessionID, answer.TargetGeneration = id, generation
		} else {
			answer.Code = "handoff_outcome_unknown"
		}
		encoded, err := json.Marshal(answer)
		if err != nil || s.Receipts.CompleteReceipt(context.WithoutCancel(ctx), receiptKey,
			store.ReceiptAnswer{Status: 200, Body: encoded}) != nil {
			answer.Code, answer.MachineExecution = "handoff_outcome_unknown", "unknown"
			answer.TargetSessionID, answer.TargetGeneration = "", ""
		}
	}
	return answer
}

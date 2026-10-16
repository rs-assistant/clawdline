package agenthandoff

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func validTaskStart() (TaskStart, []byte) {
	body := []byte("Read-only disk inventory")
	sum := sha256.Sum256(body)
	return TaskStart{
		TaskID: "task-1", RequestID: "request-1",
		Source: Endpoint{MachineID: "mac-1", SessionID: "session-1",
			ExecutionGeneration: "0123456789abcdef0123456789abcdef"},
		TargetMachineID: "linux-1", TargetProjectID: "clawdline",
		PairID: "pair-1", BodyDigest: hex.EncodeToString(sum[:]),
	}, body
}

func TestTaskStartIsMachineTargetedAndBoundToItsBody(t *testing.T) {
	start, body := validTaskStart()
	if err := ValidateTaskStart(start, body); err != nil {
		t.Fatalf("valid start refused: %v", err)
	}
	changed := []byte("Delete the disk inventory")
	if err := ValidateTaskStart(start, changed); err != InvalidRequest {
		t.Fatalf("changed body was admitted: %v", err)
	}
	start.TargetMachineID = start.Source.MachineID
	if err := ValidateTaskStart(start, body); err != InvalidRequest {
		t.Fatalf("same-machine start was admitted: %v", err)
	}
}

func TestTaskStartReceiptConflictsAcrossTargetProjectAndExecution(t *testing.T) {
	start, _ := validTaskStart()
	actor, key, first := TaskStartReceiptIdentity(start)
	changed := start
	changed.TargetProjectID = "another-project"
	a, k, second := TaskStartReceiptIdentity(changed)
	if a != actor || k != key || first == second {
		t.Fatalf("project change did not conflict in the same receipt lane")
	}
	changed = start
	changed.Source.ExecutionGeneration = "fedcba9876543210fedcba9876543210"
	a, k, second = TaskStartReceiptIdentity(changed)
	if a != actor || k != key || first == second {
		t.Fatalf("source restart did not conflict in the same receipt lane")
	}
}

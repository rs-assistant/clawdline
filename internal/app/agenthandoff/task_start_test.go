package agenthandoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	contract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

func taskStartFixture() (contract.TaskStart, []byte, contract.Principal, contract.TaskStartFacts) {
	body := []byte("Read-only disk inventory")
	sum := sha256.Sum256(body)
	start := contract.TaskStart{TaskID: "task-1", RequestID: "request-1",
		Source: contract.Endpoint{MachineID: "mac-1", SessionID: "session-1",
			ExecutionGeneration: "0123456789abcdef0123456789abcdef"},
		TargetMachineID: "linux-1", TargetProjectID: "clawdline", PairID: "pair-1",
		BodyDigest: hex.EncodeToString(sum[:])}
	principal := contract.Principal{MachineID: "mac-1", KeyFingerprint: "mac-key",
		PairID: "pair-1", MachinePeer: true}
	facts := contract.TaskStartFacts{LocalMachineID: "linux-1", PairActive: true,
		SourceCurrent: true, ProjectAvailable: true, MachineWritesAllowed: true,
		Now: time.Unix(1000, 0)}
	return start, body, principal, facts
}

func TestTaskStarterOpensOnceAcrossReplayAndChangedProject(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, body, principal, facts := taskStartFixture()
	opened := 0
	starter := TaskStarter{Receipts: s, Now: func() time.Time { return facts.Now },
		Facts: func(context.Context, contract.TaskStart, contract.Principal) (contract.TaskStartFacts, error) {
			return facts, nil
		},
		Open: func(context.Context, contract.TaskStart, []byte) (string, string, error) {
			opened++
			return "target-session", "fedcba9876543210fedcba9876543210", nil
		}}
	first := starter.Start(context.Background(), start, body, principal)
	if first.Code != "ok" || first.TargetSessionID != "target-session" || opened != 1 {
		t.Fatalf("first start: %+v, opened=%d", first, opened)
	}
	if again := starter.Start(context.Background(), start, body, principal); again != first || opened != 1 {
		t.Fatalf("replay: %+v, opened=%d", again, opened)
	}
	changed := start
	changed.TargetProjectID = "other-project"
	if got := starter.Start(context.Background(), changed, body, principal); got.Code != "handoff_request_id_reused" || opened != 1 {
		t.Fatalf("changed Project: %+v, opened=%d", got, opened)
	}
}

func TestTaskStarterRechecksRevocationBeforeOpening(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, body, principal, facts := taskStartFixture()
	reads, opened := 0, 0
	starter := TaskStarter{Receipts: s, Now: func() time.Time { return facts.Now },
		Facts: func(context.Context, contract.TaskStart, contract.Principal) (contract.TaskStartFacts, error) {
			reads++
			if reads == 2 {
				facts.PairActive = false
			}
			return facts, nil
		},
		Open: func(context.Context, contract.TaskStart, []byte) (string, string, error) {
			opened++
			return "", "", errors.New("unknown opening")
		}}
	if got := starter.Start(context.Background(), start, body, principal); got.Code != string(contract.PairInactive) || opened != 0 {
		t.Fatalf("revoked before effect: %+v, opened=%d", got, opened)
	}
	if reads != 2 {
		t.Fatalf("authority reads = %d, want 2", reads)
	}
}

func TestTaskStarterDoesNotRepeatAnUnknownOpen(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, body, principal, facts := taskStartFixture()
	opened := 0
	starter := TaskStarter{Receipts: s, Now: func() time.Time { return facts.Now },
		Facts: func(context.Context, contract.TaskStart, contract.Principal) (contract.TaskStartFacts, error) {
			return facts, nil
		},
		Open: func(context.Context, contract.TaskStart, []byte) (string, string, error) {
			opened++
			return "", "", errors.New("opening may have happened")
		}}
	for i := 0; i < 2; i++ {
		got := starter.Start(context.Background(), start, body, principal)
		if got.Code != "handoff_outcome_unknown" || got.MachineExecution != "unknown" {
			t.Fatalf("unknown open %d: %+v", i, got)
		}
	}
	if opened != 1 {
		t.Fatalf("unknown opening was attempted %d times", opened)
	}
}

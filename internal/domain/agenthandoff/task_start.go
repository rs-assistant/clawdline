package agenthandoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"
	"unicode/utf8"
)

var taskIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// TaskStart is addressed to a machine because its target Session does not
// exist yet. It must never be admitted through the Session-targeted Request
// path. The receiver resolves TargetProjectID against its own Project roster.
type TaskStart struct {
	TaskID          string   `json:"task_id"`
	RequestID       string   `json:"request_id"`
	Source          Endpoint `json:"source"`
	TargetMachineID string   `json:"target_machine_id"`
	TargetProjectID string   `json:"target_project_id"`
	PairID          string   `json:"pair_id"`
	BodyDigest      string   `json:"body_digest"`
}

// TaskStartFacts are freshly read at the target before and after the durable
// claim. PairActive includes Cloud revocation; ProjectAvailable comes from the
// target's local Project registry, never a path supplied by the source.
type TaskStartFacts struct {
	Principal            Principal
	LocalMachineID       string
	PairActive           bool
	SourceCurrent        bool
	ProjectAvailable     bool
	MachineWritesAllowed bool
	Now                  time.Time
}

// AdmitTaskStart is separate from Admit: there is no target Session execution
// to verify until after the effect opens one.
func AdmitTaskStart(start TaskStart, body []byte, facts TaskStartFacts) error {
	if err := ValidateTaskStart(start, body); err != nil {
		return err
	}
	if !facts.Principal.MachinePeer || facts.Principal.MachineID != start.Source.MachineID ||
		facts.Principal.PairID != start.PairID || facts.Principal.KeyFingerprint == "" {
		return SourceUnverified
	}
	if facts.LocalMachineID != start.TargetMachineID {
		return WrongMachine
	}
	if !facts.PairActive {
		return PairInactive
	}
	if !facts.MachineWritesAllowed {
		return MachineWritesDisabled
	}
	if !facts.SourceCurrent {
		return ExecutionUnverified
	}
	if !facts.ProjectAvailable {
		return TaskProjectUnavailable
	}
	return nil
}

// ValidateTaskStart checks immutable syntax and the plaintext binding before
// either machine checks its current, revocable authority. A source Session is
// mandatory; a target Session is deliberately absent.
func ValidateTaskStart(start TaskStart, body []byte) error {
	sum := sha256.Sum256(body)
	if ValidateTaskStartHeader(start) != nil ||
		len(body) == 0 || len(body) > MaxBodyBytes || !utf8.Valid(body) ||
		start.BodyDigest != hex.EncodeToString(sum[:]) {
		return InvalidRequest
	}
	if _, err := ParseTaskBody(body); err != nil {
		return InvalidRequest
	}
	return nil
}

// ValidateTaskStartHeader is also used by the signed source challenge, whose
// receiver does not have the encrypted task body.
func ValidateTaskStartHeader(start TaskStart) error {
	if !taskIDShape.MatchString(start.TaskID) || !validSegment(start.RequestID) ||
		!validEndpoint(start.Source) || !validSegment(start.TargetMachineID) ||
		!validSegment(start.TargetProjectID) || !validSegment(start.PairID) ||
		start.Source.MachineID == start.TargetMachineID ||
		!digest.MatchString(start.BodyDigest) {
		return InvalidRequest
	}
	return nil
}

// TaskStartReceiptIdentity keeps an identical request in one source-to-target
// lane. Reusing the ID with a changed Project, execution or body conflicts
// with the first durable claim instead of starting another Session.
func TaskStartReceiptIdentity(start TaskStart) (actor, key, requestDigest string) {
	actorBytes, _ := json.Marshal([]string{
		start.Source.MachineID, start.Source.SessionID, start.TargetMachineID,
	})
	requestBytes, _ := json.Marshal(start)
	sum := sha256.Sum256(requestBytes)
	return string(actorBytes), start.RequestID, hex.EncodeToString(sum[:])
}

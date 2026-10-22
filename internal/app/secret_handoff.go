package app

import (
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Secret Handoff keeps a person's value outside the store, Note, transcript,
// and receipts. A daemon restart intentionally loses every pending value.
const (
	MaxSecretHandoffBytes           = 16 << 10
	MaxSecretHandoffPending         = 128
	MaxSecretHandoffSeconds         = 15 * 60
	MaxSecretHandoffDeliverySeconds = 5
	// The receiving command a Secret Handoff Note declares: at most this many
	// argv entries, and at most this many bytes across all of them. It is
	// shown whole to the person, so it stays short enough to read.
	MaxSecretHandoffCommandArgs  = 32
	MaxSecretHandoffCommandBytes = 2 << 10
)

var (
	ErrSecretTooLarge    = errors.New("secret_too_large")
	ErrSecretEmpty       = errors.New("secret_empty")
	ErrSecretFull        = errors.New("secret_handoffs_full")
	ErrSecretUnavailable = errors.New("secret_unavailable")
)

// ValidateSecretHandoffCommand checks the argv a Secret Handoff Note declares
// will receive the value. The person reads it before handing off, so it must
// say plainly which program runs: argv[0] is an absolute, already-clean path
// (the CLI resolves a bare name with exec.LookPath in the Agent's own
// environment before it creates the Note), and no entry may hold a control or
// invisible formatting character that could make the shown command read as
// something other than what runs.
func ValidateSecretHandoffCommand(argv []string) error {
	if len(argv) == 0 {
		return humanInterventionError("secret_command_required", "A Secret Handoff Note must name the exact command that will receive the value.")
	}
	if len(argv) > MaxSecretHandoffCommandArgs {
		return humanInterventionError("secret_command_too_large", "The receiving command has too many arguments.")
	}
	total := 0
	for _, arg := range argv {
		total += len(arg)
		if !utf8.ValidString(arg) {
			return humanInterventionError("secret_command_invalid", "The receiving command must be valid text without control or invisible characters.")
		}
		for _, r := range arg {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return humanInterventionError("secret_command_invalid", "The receiving command must be valid text without control or invisible characters.")
			}
		}
	}
	if total > MaxSecretHandoffCommandBytes {
		return humanInterventionError("secret_command_too_large", "The receiving command is too long.")
	}
	if !filepath.IsAbs(argv[0]) || filepath.Clean(argv[0]) != argv[0] {
		return humanInterventionError("secret_command_not_absolute", "The receiving command must start with the absolute path of its executable.")
	}
	return nil
}

// SameSecretHandoffCommand is the claim's comparison: the whole argv, entry
// by entry, byte for byte. A Note that declared nothing (one written before
// commands were declared) matches nothing.
func SameSecretHandoffCommand(declared, presented []string) bool {
	return len(declared) > 0 && slices.Equal(declared, presented)
}

type secretHandoffValue struct {
	conversation string
	value        []byte
	expires      time.Time
}

type SecretHandoffs struct {
	mu      sync.Mutex
	values  map[string]secretHandoffValue
	claimed map[string]bool
	Now     func() time.Time
}

func NewSecretHandoffs() *SecretHandoffs {
	return &SecretHandoffs{values: make(map[string]secretHandoffValue), claimed: make(map[string]bool), Now: time.Now}
}

func wipeSecret(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func (h *SecretHandoffs) expire(now time.Time) {
	for id, entry := range h.values {
		if !now.Before(entry.expires) {
			wipeSecret(entry.value)
			delete(h.values, id)
		}
	}
}

// Put replaces an unclaimed value. The caller has checked that the Note is an
// open Secret Handoff addressed to conversation. No value is returned.
func (h *SecretHandoffs) Put(id, conversation string, value []byte) error {
	if len(value) == 0 {
		return ErrSecretEmpty
	}
	if len(value) > MaxSecretHandoffBytes {
		return ErrSecretTooLarge
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	if h.claimed[id] {
		return ErrSecretUnavailable
	}
	if _, exists := h.values[id]; !exists && len(h.values) >= MaxSecretHandoffPending {
		return ErrSecretFull
	}
	if old, exists := h.values[id]; exists {
		wipeSecret(old.value)
	}
	h.values[id] = secretHandoffValue{conversation: conversation, value: append([]byte(nil), value...), expires: h.Now().Add(MaxSecretHandoffSeconds * time.Second)}
	time.AfterFunc(MaxSecretHandoffSeconds*time.Second, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.expire(h.Now())
	})
	return nil
}

// Pending reports only whether an unclaimed value is waiting for this
// conversation. It never exposes the value or its size.
func (h *SecretHandoffs) Pending(id, conversation string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	entry, ok := h.values[id]
	return ok && entry.conversation == conversation && !h.claimed[id]
}

// Take is a single-use claim. It returns a detached byte slice for the local
// command to pipe to a tool; that command must wipe it after writing.
func (h *SecretHandoffs) Take(id, conversation string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	entry, exists := h.values[id]
	if !exists || entry.conversation != conversation || h.claimed[id] {
		return nil, ErrSecretUnavailable
	}
	delete(h.values, id)
	h.claimed[id] = true
	return entry.value, nil
}

// Discard wipes an unclaimed value without handing it to anyone. It is used
// when the target Session asks for the value with a command other than the
// one the person saw. It reports whether a value was waiting.
func (h *SecretHandoffs) Discard(id, conversation string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	entry, exists := h.values[id]
	if !exists || entry.conversation != conversation {
		return false
	}
	wipeSecret(entry.value)
	delete(h.values, id)
	return true
}

// ForgetClaim drops the concurrency guard only after the durable Note has
// been marked handled. A handled Note cannot accept another submission.
func (h *SecretHandoffs) ForgetClaim(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.claimed, id)
}

// Usage reports counts only; the values and their identifiers stay private.
func (h *SecretHandoffs) Usage() (pending int64, largest int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expire(h.Now())
	for _, entry := range h.values {
		pending++
		if int64(len(entry.value)) > largest {
			largest = int64(len(entry.value))
		}
	}
	return pending, largest
}

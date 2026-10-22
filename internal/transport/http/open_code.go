package http

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/auth"
)

// `clawdline open` hands a browser on this machine its sign-in through the
// browser's command line: `xdg-open URL`, `open URL`, and then the browser's
// own argv, for as long as that browser runs. On Linux every user can read
// every process's command line (`ps`, /proc/<pid>/cmdline), so whatever is in
// that address is readable by everyone on the machine. It used to be the
// browser device's token itself — a key good until somebody revoked it.
//
// The address now carries a one-time code instead. The page trades it for the
// cookie at /v1/auth/adopt; the code then no longer exists, and one that is
// never traded lapses after MaxOpenCodeSeconds. The token stays where it was
// only ever needed: in this daemon, in the cookie, and in the answer to the
// local-token holder that asked (`contract.BrowserDevice.Token`).
const (
	// MaxOpenCodeSeconds is how long an address `clawdline open` made may
	// still be opened: long enough for a browser to start cold, or for the
	// address `clawdline setup` prints to be clicked; short enough that one
	// left in a terminal's scrollback is not a key.
	MaxOpenCodeSeconds = 5 * 60
	// MaxOpenCodesPending is how many such addresses may wait to be opened at
	// once. Each is a device already added, so past it `open` is refused
	// rather than a device being added nobody can sign in with.
	MaxOpenCodesPending = 16
)

var errOpenCodesFull = errors.New("too many browser addresses are waiting to be opened")

// openCodes is the codes waiting to be traded, by their hash.
type openCodes struct {
	mu      sync.Mutex
	now     func() time.Time
	pending map[string]openCode
}

type openCode struct {
	token   string
	expires time.Time
}

func (o *openCodes) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// prune drops what has lapsed. Called with mu held.
func (o *openCodes) prune(now time.Time) {
	for k, c := range o.pending {
		if !now.Before(c.expires) {
			delete(o.pending, k)
		}
	}
}

// room is whether one more code may be minted now.
func (o *openCodes) room() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.prune(o.clock())
	return len(o.pending) < MaxOpenCodesPending
}

// mint is a fresh code for token.
func (o *openCodes) mint(token string) (string, error) {
	code, err := auth.NewToken(rand.Reader)
	if err != nil {
		return "", err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.clock()
	o.prune(now)
	if len(o.pending) >= MaxOpenCodesPending {
		return "", errOpenCodesFull
	}
	if o.pending == nil {
		o.pending = map[string]openCode{}
	}
	o.pending[auth.Hash(code)] = openCode{token: token, expires: now.Add(MaxOpenCodeSeconds * time.Second)}
	return code, nil
}

// redeem is the token a code stood for, once: the code is gone after the
// first ask, whatever the answer.
func (o *openCodes) redeem(code string) (string, bool) {
	if code == "" {
		return "", false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	key := auth.Hash(code)
	c, ok := o.pending[key]
	delete(o.pending, key)
	if !ok || !o.clock().Before(c.expires) {
		return "", false
	}
	return c.token, true
}

// waiting is how many codes wait now, for the capacity register.
func (o *openCodes) waiting() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.prune(o.clock())
	return len(o.pending)
}

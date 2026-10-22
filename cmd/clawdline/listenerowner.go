package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/sockowner"
)

// Every command that talks to the daemon sends a credential: this machine's
// token, the orchestrator's, a task secret. On a machine with other users,
// any of them can bind the daemon's port first — 127.0.0.1:7727 is anybody's
// to take while no daemon holds it — and would then be handed the credential
// by the first command that knocked. So before a request goes out on a
// loopback connection, the CLI asks the kernel who owns the socket at the
// other end of *that* connection, and refuses one owned by another user.
//
// The check is on the connection itself, not on a listing made beforehand:
// the server's end of a connection, accepted or still half-open, names the
// listening socket's owner, so there is no moment between asking and sending
// for somebody else's listener to slip into.
//
// It is installed on http.DefaultTransport, which is what every daemon call in
// this command uses (broker.go, auth.go, devices.go, task.go and the rest
// build their clients without a Transport of their own), so one place covers
// them all. Connections to anywhere but loopback are not checked: another
// machine is not a socket this machine can name.
//
// macOS and Windows: sockowner does not answer there, and the CLI sends as it
// always did (internal/adapters/sockowner/sockowner_other.go).

func init() {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		guardLoopbackDials(t)
	}
}

// errListenerNotOurs is a loopback port held by another user's socket.
var errListenerNotOurs = errors.New("held by another user")

// listenerError says which port and whose.
type listenerError struct {
	addr      string
	uid, self uint32
	err       error
}

func (e *listenerError) Error() string {
	if e.err != nil {
		return fmt.Sprintf(cliCopy("misc", "listener.unknown",
			"could not tell which user is listening on %s (%v), so no credential was sent there"), e.addr, e.err)
	}
	return fmt.Sprintf(cliCopy("misc", "listener.other_user",
		"%s is held by a process of another user (uid %d), not by your Clawdline daemon (uid %d), so no credential "+
			"was sent there. Stop whatever holds that port, or run your daemon on another one (CLAWDLINE_NEXT_PORT)"),
		e.addr, e.uid, e.self)
}

func (e *listenerError) Unwrap() error {
	if e.err != nil {
		return e.err
	}
	return errListenerNotOurs
}

// guardLoopbackDials makes every loopback connection t opens prove that this
// user owns the far end before anything is written to it.
func guardLoopbackDials(t *http.Transport) {
	dial := t.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if err := ownedByThisUser(c, uint32(os.Getuid())); err != nil {
			_ = c.Close()
			return nil, err
		}
		return c, nil
	}
}

// listenerOwner is sockowner.Owner, replaceable by a test.
var listenerOwner = sockowner.Owner

// ownedByThisUser is nil when c's far end is this user's socket, another
// machine's, or on a platform that cannot tell.
func ownedByThisUser(c net.Conn, self uint32) error {
	server, okServer := sockowner.AddrPort(c.RemoteAddr())
	client, okClient := sockowner.AddrPort(c.LocalAddr())
	if !okServer || !okClient || !server.Addr().IsLoopback() {
		return nil
	}
	// The server's end of this connection: bound to the address dialled,
	// connected to ours.
	uid, err := listenerOwner(server, client)
	switch {
	case errors.Is(err, sockowner.ErrUnsupported):
		return nil
	case err != nil:
		return &listenerError{addr: server.String(), self: self, err: err}
	case uid != self:
		return &listenerError{addr: server.String(), uid: uid, self: self}
	}
	return nil
}

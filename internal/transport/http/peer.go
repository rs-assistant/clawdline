package http

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/sockowner"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// Who is on the other end of a connection, for a machine with more than one
// user.
//
// A browser keeps one cookie per host whatever the port. So a service another
// user runs on some other 127.0.0.1 port, visited in the browser signed in to
// this console, is sent this daemon's cookie, and can replay it to this port
// from its own process: the Origin and Sec-Fetch checks in gate.go stop a
// *page* elsewhere, not a process holding the cookie. A process of the
// person's own could read the token files anyway, so the boundary worth
// drawing is the user: on Linux the kernel says which uid owns the socket a
// connection came from, and a credential that arrives from a socket another
// user owns is refused, however good the credential is.
//
// What is not a connection to this listener is not judged here. A Cloud
// request is answered in process (internal/transport/cloud LocalAuthorizer),
// and a test calls the handler directly; neither has a peer, and both are
// judged by their credentials as before. A tunnel's cloudflared is started by
// this daemon, so it is this user's process, and what it carries is judged by
// credential as before too. A connection from another machine has no socket
// in this machine's table and is a remote asker, as it always was.
//
// macOS and Windows: sockowner does not answer there, every peer is admitted,
// and another user's replayed cookie is not refused (sockowner_other.go).

// peerConn is one accepted connection, judged at most once, on the first
// request that needs the answer.
type peerConn struct {
	local, remote net.Addr
	once          sync.Once
	verdict       peerVerdict
}

// peerVerdict is what the kernel said about the connecting socket.
type peerVerdict struct {
	// admit is a peer that may present a credential: this user, another
	// machine, or a platform that cannot tell.
	admit bool
	// uid is the other user's, when the kernel named one.
	uid  uint32
	err  error
	kind peerKind
}

type peerKind int

const (
	peerSelf peerKind = iota
	peerRemote
	peerUnchecked
	peerOtherUser
	peerUnknown
)

type peerKey struct{}

// connContext is http.Server.ConnContext: it gives every accepted connection
// a place for its peer's verdict.
func connContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, peerKey{}, &peerConn{local: c.LocalAddr(), remote: c.RemoteAddr()})
}

// peerOf is the connection a request came over, or nil for a request this
// daemon's listener did not accept.
func peerOf(ctx context.Context) *peerConn {
	p, _ := ctx.Value(peerKey{}).(*peerConn)
	return p
}

// socketOwner is sockowner.Owner, replaceable by a test that needs /proc to fail.
var socketOwner = sockowner.Owner

func (p *peerConn) judge() peerVerdict {
	p.once.Do(func() { p.verdict = judgePeer(p.local, p.remote, uint32(os.Getuid())) })
	return p.verdict
}

func judgePeer(local, remote net.Addr, self uint32) peerVerdict {
	server, okServer := sockowner.AddrPort(local)
	client, okClient := sockowner.AddrPort(remote)
	if !okServer || !okClient {
		return peerVerdict{kind: peerUnknown, err: fmt.Errorf("the connection is not TCP (%v from %v)", local, remote)}
	}
	uid, err := socketOwner(client, server)
	switch {
	case errors.Is(err, sockowner.ErrUnsupported):
		return peerVerdict{admit: true, kind: peerUnchecked}
	case errors.Is(err, sockowner.ErrNotFound) && !client.Addr().IsLoopback():
		// Not a socket of this machine: somebody on the network, judged by
		// credential as a remote asker always was.
		return peerVerdict{admit: true, kind: peerRemote}
	case err != nil:
		return peerVerdict{kind: peerUnknown, err: err}
	case uid != self:
		return peerVerdict{kind: peerOtherUser, uid: uid}
	}
	return peerVerdict{admit: true, kind: peerSelf}
}

// peerRefusal is the 403 for a request whose connection may not present a
// credential, or "" when it may go on to be judged by its credential.
//
// A peer that is not this user is answered what anybody without a credential
// is answered, and nothing else: the console's own files, and the open routes
// asked without a credential. It may not present one — a header, the cookie,
// the orchestrator's, a task secret, or a token handed to /v1/auth/adopt.
func peerRefusal(r *http.Request, p string) string {
	conn := peerOf(r.Context())
	if conn == nil {
		return ""
	}
	v := conn.judge()
	if v.admit {
		return ""
	}
	if openPath(p) && !strings.HasPrefix(p, "/v1/") {
		// The bundle is the same for everybody, cookie or none.
		return ""
	}
	if openPath(p) && !presentsCredential(r, p) {
		return ""
	}
	if v.kind == peerOtherUser {
		notePeer(&peerOtherLogged, "auth: a credential from a process of uid %d was refused: only this daemon's own user may present one", v.uid)
		return fmt.Sprintf("This connection comes from a process of another user on this machine (uid %d), "+
			"and this daemon takes a credential only from its own user's processes.", v.uid)
	}
	notePeerUnknown(v.err)
	return "This daemon could not tell which user on this machine opened this connection, so it does not take " +
		"a credential on it. Its log says why."
}

// presentsCredential is whether a request carries, or hands over, anything
// that would let it in.
func presentsCredential(r *http.Request, p string) bool {
	if bearer(r) != "" || r.Header.Get(machineHeader) != "" || r.Header.Get(orchestrator.HeaderTaskSecret) != "" {
		return true
	}
	if taskSecretRoute(r.Method, p) {
		return true
	}
	// A token or a one-time code in the body, traded for the cookie.
	return p == "/v1/auth/adopt"
}

// peerUnknownLogged and peerOtherLogged are when each refusal was last written
// to the log: once a minute, so a console that cannot be let in, or another
// user replaying a cookie, says so without filling the log.
var peerUnknownLogged, peerOtherLogged atomic.Int64

func notePeerUnknown(err error) {
	notePeer(&peerUnknownLogged, "auth: a credential was refused because the user behind its loopback connection could not be "+
		"read from /proc/net/tcp: %v. Every local request with a credential is refused while this lasts; "+
		"check that /proc is mounted and readable by this user", err)
}

func notePeer(last *atomic.Int64, format string, args ...any) {
	now := time.Now().Unix()
	before := last.Load()
	if now-before < 60 || !last.CompareAndSwap(before, now) {
		return
	}
	log.Printf(format, args...)
}

// Package othersock makes TCP sockets that another user owns, for tests of
// the checks that refuse such sockets (internal/adapters/sockowner). Only
// root on Linux can: the socket is made while the thread's filesystem uid is
// the other user's, which is the uid the kernel records on a new socket and
// shows in /proc/net/tcp. Everywhere else every function is ErrUnavailable,
// and the test that asked skips.
package othersock

import "errors"

// ErrUnavailable is a test process that cannot make another user's socket.
var ErrUnavailable = errors.New("only root on Linux can make a socket another user owns")

// Nobody is the uid the tests act as: the conventional unprivileged user.
const Nobody = 65534

//go:build !linux

package sockowner

import "net/netip"

// Supported is whether Owner can answer on this platform.
//
// macOS and Windows do not: neither has a table a process can read that names
// the user behind another process's TCP socket without privileges (macOS
// would need libproc over every process, Windows GetExtendedTcpTable plus a
// token per owning process). So on those platforms the daemon does not refuse
// a cookie replayed by another user's process, and the CLI does not check who
// holds the daemon's port before it sends a token there.
const Supported = false

// Owner is ErrUnsupported here; see Supported.
func Owner(local, remote netip.AddrPort) (uint32, error) {
	return 0, ErrUnsupported
}

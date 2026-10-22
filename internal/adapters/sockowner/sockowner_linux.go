package sockowner

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
)

// Supported is whether Owner can answer on this platform.
const Supported = true

// procNet is where the kernel lists this network namespace's sockets.
var procNet = "/proc/net"

// Owner answers the uid that owns the TCP socket bound to local and connected
// to remote — for a connection a client made, local is the client's address
// and remote the server's. A table that cannot be read is an error, never "no
// such socket": a caller deciding whether to trust a connection must not take
// an unreadable /proc for an answer.
//
// The uid is the one the kernel shows to this process's user namespace.
func Owner(local, remote netip.AddrPort) (uint32, error) {
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(procNet, name))
		if name == "tcp6" && errors.Is(err, fs.ErrNotExist) {
			// A kernel without IPv6 has no tcp6 table, and so no IPv6
			// socket to find in it.
			continue
		}
		if err != nil {
			return 0, err
		}
		uid, ok, err := scan(f, binary.NativeEndian, local, remote)
		_ = f.Close()
		if err != nil {
			return 0, err
		}
		if ok {
			return uid, nil
		}
	}
	return 0, ErrNotFound
}

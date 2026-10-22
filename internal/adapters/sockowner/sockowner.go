// Package sockowner says which user owns one end of a TCP connection on this
// machine.
//
// Both sides of this daemon need it on a machine with more than one user. A
// browser keeps one cookie per host whatever the port, so another user's
// service on another 127.0.0.1 port is sent the console's cookie and can
// replay it to the daemon from its own process; the daemon asks who owns the
// connecting socket (transport/http peer.go). And another user can bind the
// daemon's port first; the CLI asks who owns the socket it reached before it
// sends a token there (cmd/clawdline listenerowner.go).
//
// Only Linux answers, from /proc/net/tcp and /proc/net/tcp6. Elsewhere Owner
// is ErrUnsupported, and callers say what that leaves uncovered.
package sockowner

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var (
	// ErrUnsupported is a platform that does not say who owns a socket.
	ErrUnsupported = errors.New("this platform does not say which user owns a TCP socket")
	// ErrNotFound is no socket with that local and remote address: the
	// connection is not between two sockets of this machine, or it closed.
	ErrNotFound = errors.New("no TCP socket on this machine has that local and remote address")
)

// AddrPort is a net.Addr as a netip.AddrPort, for a TCP address only.
func AddrPort(a net.Addr) (netip.AddrPort, bool) {
	tcp, ok := a.(*net.TCPAddr)
	if !ok || tcp == nil {
		return netip.AddrPort{}, false
	}
	ap := tcp.AddrPort()
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), ap.IsValid()
}

// tcpTimeWait is the state a closed connection lingers in. Its row has no
// socket behind it, and the kernel prints uid 0 for it, so it names nobody.
const tcpTimeWait = 0x06

// scan reads one /proc/net/tcp-format table for the socket bound to local and
// connected to remote, and answers its uid. order is the byte order the
// kernel printed the addresses in, which is the machine's own.
//
// A row is
//
//	sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode ...
//
// where an address is the IP's bytes as hexadecimal 32-bit words in host
// order, a colon, and the port in hexadecimal.
func scan(r io.Reader, order binary.ByteOrder, local, remote netip.AddrPort) (uint32, bool, error) {
	local = netip.AddrPortFrom(local.Addr().Unmap(), local.Port())
	remote = netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port())
	lines := bufio.NewScanner(r)
	first := true
	for lines.Scan() {
		if first {
			// The header.
			first = false
			continue
		}
		fields := strings.Fields(lines.Text())
		if len(fields) < 8 {
			continue
		}
		l, err := parseAddr(fields[1], order)
		if err != nil {
			return 0, false, err
		}
		if l != local {
			continue
		}
		rem, err := parseAddr(fields[2], order)
		if err != nil {
			return 0, false, err
		}
		if rem != remote {
			continue
		}
		state, err := strconv.ParseUint(fields[3], 16, 8)
		if err != nil {
			return 0, false, fmt.Errorf("a socket state %q: %w", fields[3], err)
		}
		if state == tcpTimeWait {
			continue
		}
		uid, err := strconv.ParseUint(fields[7], 10, 32)
		if err != nil {
			return 0, false, fmt.Errorf("a socket uid %q: %w", fields[7], err)
		}
		return uint32(uid), true, nil
	}
	return 0, false, lines.Err()
}

// parseAddr reads "0100007F:1E2F" or its 32-hex-digit IPv6 form.
func parseAddr(field string, order binary.ByteOrder) (netip.AddrPort, error) {
	host, port, ok := strings.Cut(field, ":")
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("a socket address %q", field)
	}
	raw, err := hex.DecodeString(host)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.AddrPort{}, fmt.Errorf("a socket address %q", field)
	}
	// Each 32-bit word was printed as a number in host order; written back
	// in host order it is the address's bytes again.
	ip := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		order.PutUint32(ip[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	p, err := strconv.ParseUint(port, 16, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("a socket port %q", field)
	}
	addr, _ := netip.AddrFromSlice(ip)
	return netip.AddrPortFrom(addr.Unmap(), uint16(p)), nil
}

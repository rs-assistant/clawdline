//go:build !linux

package othersock

import (
	"net"
	"net/netip"
)

// Dial is ErrUnavailable off Linux.
func Dial(uid int, addr netip.AddrPort) (net.Conn, error) { return nil, ErrUnavailable }

// Listen is ErrUnavailable off Linux.
func Listen(uid int, port int) (net.Listener, error) { return nil, ErrUnavailable }

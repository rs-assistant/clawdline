package othersock

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"syscall"
)

// Dial connects to addr (IPv4) from a socket uid owns.
func Dial(uid int, addr netip.AddrPort) (net.Conn, error) {
	fd, err := asUser(uid, func() (int, error) {
		return syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	})
	if err != nil {
		return nil, err
	}
	sa := &syscall.SockaddrInet4{Port: int(addr.Port()), Addr: addr.Addr().As4()}
	if err := syscall.Connect(fd, sa); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("connect: %w", err)
	}
	f := os.NewFile(uintptr(fd), "othersock-conn")
	defer f.Close()
	return net.FileConn(f)
}

// Listen listens on 127.0.0.1 at port (0 for any) from a socket uid owns, and
// accepts as uid too.
//
// Both matter: the kernel stamps an accepted socket with the accepting
// thread's uid (sock_graft), so a listener of uid's accepted by this root
// process would hand back connections that root owns — which is not what
// another user's service looks like.
func Listen(uid int, port int) (net.Listener, error) {
	fd, err := asUser(uid, func() (int, error) {
		return syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	})
	if err != nil {
		return nil, err
	}
	sa := &syscall.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}
	if err := syscall.Bind(fd, sa); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("bind: %w", err)
	}
	if err := syscall.Listen(fd, 16); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("listen: %w", err)
	}
	bound, err := syscall.Getsockname(fd)
	if err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("getsockname: %w", err)
	}
	in4 := bound.(*syscall.SockaddrInet4)
	return &listener{fd: fd, uid: uid, addr: &net.TCPAddr{IP: net.IP(in4.Addr[:]), Port: in4.Port}}, nil
}

// listener is a blocking listening socket outside Go's poller, so that each
// accept runs on a thread whose filesystem uid is the owner's.
type listener struct {
	fd   int
	uid  int
	addr *net.TCPAddr
}

func (l *listener) Accept() (net.Conn, error) {
	nfd, err := asUser(l.uid, func() (int, error) {
		nfd, _, err := syscall.Accept4(l.fd, syscall.SOCK_CLOEXEC)
		return nfd, err
	})
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(nfd), "othersock-accepted")
	defer f.Close()
	return net.FileConn(f)
}

// Close wakes a blocked Accept (shutdown on a listening socket does) and
// closes the socket.
func (l *listener) Close() error {
	_ = syscall.Shutdown(l.fd, syscall.SHUT_RDWR)
	return syscall.Close(l.fd)
}

func (l *listener) Addr() net.Addr { return l.addr }

// asUser runs make while this thread's filesystem uid is uid, then puts the
// thread back. setfsuid is per thread, so the thread is locked for the length
// of it; the uid a new socket records is the filesystem uid of the thread
// that made it.
func asUser(uid int, make func() (int, error)) (int, error) {
	if os.Geteuid() != 0 {
		return -1, ErrUnavailable
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := syscall.Setfsuid(uid); err != nil {
		return -1, fmt.Errorf("setfsuid: %w", err)
	}
	fd, err := make()
	if restore := syscall.Setfsuid(os.Geteuid()); restore != nil && err == nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("setfsuid back: %w", restore)
	}
	if err != nil {
		return -1, err
	}
	return fd, nil
}

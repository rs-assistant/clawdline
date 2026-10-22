package sockowner

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/sockowner/othersock"
)

// A real loopback connection, read back from this machine's /proc: both ends
// are this test's user.
func TestOwnerReadsThisMachinesSockets(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	c, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if s := <-accepted; s != nil {
		defer s.Close()
	}
	client, _ := AddrPort(c.LocalAddr())
	server, _ := AddrPort(c.RemoteAddr())
	me := uint32(os.Getuid())
	if uid, err := Owner(client, server); err != nil || uid != me {
		t.Fatalf("the client's end = %d, %v; want %d", uid, err, me)
	}
	if uid, err := Owner(server, client); err != nil || uid != me {
		t.Fatalf("the server's end = %d, %v; want %d", uid, err, me)
	}
}

// A socket another user made is reported as theirs, from either end.
func TestOwnerNamesAnotherUsersSocket(t *testing.T) {
	ln, err := othersock.Listen(othersock.Nobody, 0)
	if errors.Is(err, othersock.ErrUnavailable) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	client, _ := AddrPort(c.LocalAddr())
	server, _ := AddrPort(c.RemoteAddr())
	// The listener has not accepted: the kernel's row for the other end is
	// the half-open one, which still names the listener's owner.
	if uid, err := Owner(server, client); err != nil || uid != othersock.Nobody {
		t.Fatalf("the server's end = %d, %v; want %d", uid, err, othersock.Nobody)
	}

	back, err := othersock.Dial(othersock.Nobody, server)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Close()
	from, _ := AddrPort(back.LocalAddr())
	if uid, err := Owner(from, server); err != nil || uid != othersock.Nobody {
		t.Fatalf("the other user's client = %d, %v; want %d", uid, err, othersock.Nobody)
	}
}

// A /proc that cannot be read is an error, not "no such socket".
func TestOwnerDoesNotTakeAnUnreadableTableForAnAnswer(t *testing.T) {
	old := procNet
	procNet = filepath.Join(t.TempDir(), "missing")
	defer func() { procNet = old }()
	client, _ := AddrPort(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000})
	server, _ := AddrPort(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7727})
	if _, err := Owner(client, server); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("an unreadable table answered %v", err)
	}
}

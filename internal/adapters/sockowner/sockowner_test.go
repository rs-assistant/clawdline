package sockowner

import (
	"encoding/binary"
	"net/netip"
	"strings"
	"testing"
)

// Rows as a little-endian kernel prints them: 127.0.0.1:7727 listening as
// uid 1000, a client 127.0.0.1:46400 of uid 1001 connected to it, the
// accepted end of that connection, an IPv4-mapped IPv6 client, and a
// TIME_WAIT row, which the kernel prints as uid 0.
const table4 = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1E2F 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 2062 1 0000000000000000 100 0 0 10 0
   1: 0100007F:B540 0100007F:1E2F 01 00000000:00000000 00:00000000 00000000  1001        0 3001 1 0000000000000000 20 4 30 10 -1
   2: 0100007F:1E2F 0100007F:B540 01 00000000:00000000 00:00000000 00000000  1000        0 3002 1 0000000000000000 20 4 30 10 -1
   3: 0100007F:B541 0100007F:1E2F 06 00000000:00000000 03:00000000 00000000     0        0 0 3 0000000000000000
`

const table6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0000000000000000FFFF00000100007F:B542 0000000000000000FFFF00000100007F:1E2F 01 00000000:00000000 00:00000000 00000000  1002        0 4001 1 0000000000000000 20 4 30 10 -1
   1: 00000000000000000000000001000000:B543 00000000000000000000000001000000:1E2F 01 00000000:00000000 00:00000000 00000000  1003        0 4002 1 0000000000000000 20 4 30 10 -1
`

func TestScanFindsTheSocketByBothAddresses(t *testing.T) {
	for _, tc := range []struct {
		name          string
		table         string
		local, remote string
		uid           uint32
		found         bool
	}{
		{"the client's own end", table4, "127.0.0.1:46400", "127.0.0.1:7727", 1001, true},
		{"the accepted end", table4, "127.0.0.1:7727", "127.0.0.1:46400", 1000, true},
		{"a TIME_WAIT row names nobody", table4, "127.0.0.1:46401", "127.0.0.1:7727", 0, false},
		{"no such connection", table4, "127.0.0.1:46402", "127.0.0.1:7727", 0, false},
		{"an IPv4-mapped client", table6, "127.0.0.1:46402", "127.0.0.1:7727", 1002, true},
		{"an IPv6 loopback client", table6, "[::1]:46403", "[::1]:7727", 1003, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, found, err := scan(strings.NewReader(tc.table), binary.LittleEndian,
				netip.MustParseAddrPort(tc.local), netip.MustParseAddrPort(tc.remote))
			if err != nil || found != tc.found || uid != tc.uid {
				t.Fatalf("scan = %d, %v, %v; want %d, %v", uid, found, err, tc.uid, tc.found)
			}
		})
	}
}

func TestScanRefusesATableItCannotRead(t *testing.T) {
	bad := "header\n   0: NOTHEX:1E2F 0100007F:B540 01 0 0 0 1000 0 1\n"
	if _, _, err := scan(strings.NewReader(bad), binary.LittleEndian,
		netip.MustParseAddrPort("127.0.0.1:7727"), netip.MustParseAddrPort("127.0.0.1:46400")); err == nil {
		t.Fatal("a row whose address is not hexadecimal was read as no row")
	}
}

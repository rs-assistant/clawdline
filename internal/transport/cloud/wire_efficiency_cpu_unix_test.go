//go:build unix

package cloud

import (
	"syscall"
	"testing"
)

func wireBenchCPU(t *testing.T) int64 {
	t.Helper()
	var u syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
		t.Fatal(err)
	}
	return u.Utime.Nano() + u.Stime.Nano()
}

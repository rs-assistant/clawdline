//go:build !windows

package updater

import (
	"errors"
	"syscall"
)

// processAlive says whether pid names a live process; one owned by somebody
// else still counts.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

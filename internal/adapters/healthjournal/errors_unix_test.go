//go:build !windows

package healthjournal

import "syscall"

var (
	errDiskFull = syscall.ENOSPC
	errReadOnly = syscall.EROFS
)

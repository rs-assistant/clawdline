//go:build windows

package healthjournal

import "golang.org/x/sys/windows"

var (
	errDiskFull = windows.ERROR_DISK_FULL
	errReadOnly = windows.ERROR_WRITE_PROTECT
)

//go:build windows

package ownrecords

import (
	"time"

	"golang.org/x/sys/windows"
)

// ProcessStart pins first-message work to the kernel process on Windows too.
// An inaccessible or exited process supplies no identity.
func ProcessStart(pid int) time.Time {
	if pid <= 0 {
		return time.Time{}
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(handle, &created, &exited, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, created.Nanoseconds()).Truncate(time.Second)
}

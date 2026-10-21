//go:build windows

package healthjournal

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// ensureDir creates dir when absent and refuses a symlink or non-directory.
// Windows access control lists are inherited from the state directory; this
// package does not rewrite them.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &Error{Code: classify(err), Field: "dir"}
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return &Error{Code: classify(err), Field: "dir"}
	}
	if fi.Mode()&os.ModeSymlink != 0 || fi.Mode()&os.ModeIrregular != 0 || !fi.IsDir() {
		return &Error{Code: CodeUnsafeDirectory, Field: "dir"}
	}
	return nil
}

type fileLock struct{ f *os.File }

func (l fileLock) Close() error {
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, ol)
	return l.f.Close()
}

// lockDir takes an exclusive, non-blocking lock on dir/LOCK; Windows releases
// it when the holding process exits.
func lockDir(dir string) (io.Closer, error) {
	path := filepath.Join(dir, lockName)
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return nil, &Error{Code: CodeUnsafeDirectory, Field: "lock"}
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, &Error{Code: classify(err), Field: "lock"}
	}
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, fail(CodeLocked)
		}
		return nil, &Error{Code: classify(err), Field: "lock"}
	}
	return fileLock{f}, nil
}

// classify maps an operating-system error to a stable code.
func classify(err error) Code {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL):
		return CodeDiskFull
	case errors.Is(err, windows.ERROR_WRITE_PROTECT):
		return CodeReadOnly
	case errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, os.ErrPermission):
		return CodePermissionDenied
	}
	return CodeIOError
}

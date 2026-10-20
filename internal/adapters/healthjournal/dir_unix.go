//go:build !windows

package healthjournal

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// ensureDir creates dir 0700 when absent and refuses one that is a symlink,
// not a directory, owned by someone else, or writable by anyone else.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &Error{Code: classify(err), Field: "dir"}
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return &Error{Code: classify(err), Field: "dir"}
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return &Error{Code: CodeUnsafeDirectory, Field: "dir"}
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return &Error{Code: CodeUnsafeDirectory, Field: "owner"}
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return &Error{Code: CodeUnsafeDirectory, Field: "mode"}
	}
	return nil
}

type flock struct{ f *os.File }

func (l flock) Close() error {
	unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	return l.f.Close()
}

// lockDir takes an exclusive, non-blocking advisory lock on dir/LOCK. A
// second process (or a second Open) gets locked; the kernel releases the
// lock when the holder dies, so a crash never leaves the store wedged.
func lockDir(dir string) (io.Closer, error) {
	path := filepath.Join(dir, lockName)
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return nil, &Error{Code: CodeUnsafeDirectory, Field: "lock"}
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, &Error{Code: classify(err), Field: "lock"}
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fail(CodeLocked)
		}
		return nil, &Error{Code: classify(err), Field: "lock"}
	}
	return flock{f}, nil
}

// classify maps an operating-system error to a stable code.
func classify(err error) Code {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT):
		return CodeDiskFull
	case errors.Is(err, syscall.EROFS):
		return CodeReadOnly
	case errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM):
		return CodePermissionDenied
	}
	return CodeIOError
}

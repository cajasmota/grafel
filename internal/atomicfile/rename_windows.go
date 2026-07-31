//go:build windows

package atomicfile

import (
	"errors"
	"os"
	"syscall"
)

// The Win32 codes MoveFileEx returns for the two failures in rename.go.
// Declared here rather than taken from golang.org/x/sys/windows so this
// package keeps its stdlib-only dependency set (package syscall on Windows
// does not export the sharing/lock codes).
const (
	errorAccessDenied     = syscall.Errno(5)  // ERROR_ACCESS_DENIED
	errorSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	errorLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// renameErrRecoverable reports whether err is one of the two failures
// renameOver knows how to recover from. ERROR_ACCESS_DENIED covers both: it is
// what REPLACE_EXISTING returns for a read-only destination AND what a
// no-FILE_SHARE_DELETE opener produces, so the driver tries the deterministic
// remedy first and falls through to the retry.
//
// Everything else — ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND,
// ERROR_NOT_SAME_DEVICE — is a real error and must surface immediately.
func renameErrRecoverable(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case errorAccessDenied, errorSharingViolation, errorLockViolation:
		return true
	}
	return false
}

// destIsReadOnly reports whether path carries FILE_ATTRIBUTE_READONLY.
//
// Go's Windows os.Stat reports exactly two permission values — 0444 for a file
// with the read-only attribute and 0666 for one without — so the owner-write
// bit is a faithful reading of the attribute. Using os.Stat/os.Chmod rather
// than GetFileAttributes/SetFileAttributesW keeps this to the stdlib and to
// the same mapping os.Chmod uses on the way back out.
func destIsReadOnly(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false // missing destination: nothing to clear.
	}
	return fi.Mode().IsRegular() && fi.Mode().Perm()&0o200 == 0
}

// setDestReadOnly sets or clears FILE_ATTRIBUTE_READONLY on path.
func setDestReadOnly(path string, ro bool) error {
	if ro {
		return os.Chmod(path, 0o444)
	}
	return os.Chmod(path, 0o666)
}

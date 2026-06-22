//go:build windows

package main

import (
	"fmt"
	"syscall"
	"testing"
)

// TestIsWindowsFileInUseErrno verifies the locale-invariant errno path: real
// Windows file-in-use / access-denied errors carry a numeric syscall.Errno
// (winerror.h 5/32/33), matched regardless of the OS message language. This is
// the Windows complement to the cross-platform TestIsWindowsFileInUse, which
// only exercises fs.ErrPermission + the negative (localized-string) cases.
func TestIsWindowsFileInUseErrno(t *testing.T) {
	for _, errno := range []syscall.Errno{errorAccessDenied, errorSharingViolation, errorLockViolation} {
		if !isWindowsFileInUseErrno(errno) {
			t.Errorf("isWindowsFileInUseErrno(%d) = false, want true", uint(errno))
		}
		// Wrapped, as os.Remove/os.Open surface it via *os.PathError.
		wrapped := fmt.Errorf("remove C:\\x: %w", errno)
		if !isWindowsFileInUse(wrapped) {
			t.Errorf("isWindowsFileInUse(wrapped %d) = false, want true", uint(errno))
		}
	}
	// An unrelated errno must not match.
	if isWindowsFileInUseErrno(syscall.Errno(2)) { // ERROR_FILE_NOT_FOUND
		t.Error("isWindowsFileInUseErrno(2 file-not-found) = true, want false")
	}
}

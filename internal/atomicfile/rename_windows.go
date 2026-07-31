//go:build windows

package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// renameErrRecoverable reports whether err is one of the failures renameOver
// knows how to recover from.
//
// Deliberately identical to the five existing copies of this predicate
// (internal/graph/{groupalgo,descriptions,flows}.isSharingOrAccessError,
// internal/install.isAccessOrSharingError,
// internal/statusfile.isRetryableReplaceError) rather than a sixth dialect:
// same fs.ErrPermission first test, same three golang.org/x/sys/windows
// constants. An earlier draft hand-declared syscall.Errno(5/32/33) to keep
// this package stdlib-only — a bad trade, since golang.org/x/sys is already a
// direct go.mod requirement and internal/process (which this PR's reaper fix
// now leans on) imports it too.
//
// ERROR_ACCESS_DENIED covers both failure modes: it is what REPLACE_EXISTING
// returns for a read-only destination AND what a no-FILE_SHARE_DELETE opener
// produces, so renameOver tries the deterministic remedy first and falls
// through to the retry. Everything else — ERROR_FILE_NOT_FOUND,
// ERROR_PATH_NOT_FOUND, ERROR_NOT_SAME_DEVICE — is a real error and must
// surface immediately.
func renameErrRecoverable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case windows.ERROR_ACCESS_DENIED,
			windows.ERROR_SHARING_VIOLATION,
			windows.ERROR_LOCK_VIOLATION:
			return true
		}
	}
	return false
}

// destIsReadOnly reports whether path carries FILE_ATTRIBUTE_READONLY.
//
// Go's Windows os.Stat reports exactly two permission values — 0444 for a file
// with the read-only attribute and 0666 for one without — so the owner-write
// bit is a faithful reading of the attribute. Using os.Stat/os.Chmod rather
// than GetFileAttributes/SetFileAttributesW keeps the read and the write on
// one mapping: syscall.Chmod toggles FILE_ATTRIBUTE_READONLY off the S_IWRITE
// bit, which is precisely the bit tested here.
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

//go:build windows

package descriptions

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// atomicRenameRetries / atomicRenameRetryDelay bound the Windows rename-over
// retry loop. On Windows, rename-over-existing fails with
// ERROR_SHARING_VIOLATION / ERROR_ACCESS_DENIED while ANY process still holds
// the destination open — including a concurrent reader mid os.ReadFile. The
// sidecar's real reader opens, reads, and closes the file in a single brief
// operation, so a short bounded backoff rides out that window. Variables (not
// consts) so a future test can shrink the delay.
var (
	atomicRenameRetries    = 40
	atomicRenameRetryDelay = 5 * time.Millisecond
)

// atomicRename performs the temp→dest swap that finalizes a sidecar write,
// retrying on the transient Windows sharing/access violation raised when a
// concurrent reader still has the destination open. A persistent lock still
// surfaces the genuine error after the bounded loop exhausts.
func atomicRename(tmp, dst string) error {
	var err error
	for i := 0; i < atomicRenameRetries; i++ {
		err = os.Rename(tmp, dst)
		if err == nil {
			return nil
		}
		if !isSharingOrAccessError(err) {
			return err
		}
		time.Sleep(atomicRenameRetryDelay)
	}
	return err
}

// isSharingOrAccessError reports whether err is the transient Windows lock
// raised when renaming over a destination another handle still has open.
func isSharingOrAccessError(err error) bool {
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

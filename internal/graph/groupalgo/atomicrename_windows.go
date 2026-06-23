//go:build windows

package groupalgo

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// atomicRenameRetries / atomicRenameRetryDelay bound the Windows rename-over
// retry loop. Unlike Unix, Windows rename-over-existing fails with
// ERROR_SHARING_VIOLATION / ERROR_ACCESS_DENIED while ANY process still holds
// the destination open — including a concurrent reader mid os.ReadFile. The
// overlay's real reader (the MCP apply path) opens, reads, and closes the
// file in a single brief operation, so the destination is held only for a few
// microseconds at a time; a short bounded backoff rides out that window and
// lets the swap land. ~10 tries × a few ms ≈ tens of ms worst case, which is
// imperceptible for the overlay write yet survives the no-torn-read stress
// test's four tight reader loops. Variables (not consts) so a future test can
// shrink the delay.
var (
	atomicRenameRetries    = 10
	atomicRenameRetryDelay = 3 * time.Millisecond
)

// atomicRename performs the temp→dest swap that finalizes an overlay write,
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
// Mirrors install.isAccessOrSharingError (kept local to avoid an import cycle
// across the graph/install boundary).
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

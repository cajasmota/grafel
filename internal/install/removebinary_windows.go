//go:build windows

package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// removeBinaryPlatform removes the binary on Windows.
//
// Windows refuses to delete the file backing a running executable, and the
// `grafel uninstall --remove-binary` command IS that executable trying to
// delete itself, so a plain os.Remove returns ERROR_ACCESS_DENIED /
// ERROR_SHARING_VIOLATION. We handle that case with the standard
// rename-aside + delete-on-reboot dance:
//
//  1. Try os.Remove. If the binary is a *different* copy (not the running
//     process) this succeeds and we're done.
//  2. If it fails with a sharing/access error and binPath is the currently
//     running exe, rename it to a unique sibling within the same directory.
//     Renaming a running exe is allowed on Windows because it only mutates the
//     directory entry, not the locked file body. This frees the canonical
//     install path (<bin>\grafel.exe), which is what the uninstall guarantees.
//  3. Schedule the renamed orphan for deletion on next reboot via
//     MoveFileEx(.., NULL, MOVEFILE_DELAY_UNTIL_REBOOT), and best-effort try an
//     immediate remove (usually still fails while running — harmless).
//
// Returning nil here means the binary is gone from its install path; the .old
// orphan is reboot-cleaned.
func removeBinaryPlatform(binPath string) error {
	err := osRemove(binPath)
	if err == nil {
		return nil
	}
	if !isAccessOrSharingError(err) || !isRunningExecutable(binPath) {
		return err
	}

	aside := renamedAsidePath(binPath, os.Getpid())
	if rerr := os.Rename(binPath, aside); rerr != nil {
		// Could not free the install path — surface the original error so the
		// caller reports the genuine failure.
		return fmt.Errorf("self-delete: rename %s aside: %w (original: %v)", binPath, rerr, err)
	}

	// The canonical install path is now clear. Schedule the orphan for deletion
	// on reboot; this is best-effort and must not fail the uninstall.
	if derr := scheduleDeleteOnReboot(aside); derr != nil {
		fmt.Fprintf(os.Stderr,
			"grafel uninstall: binary renamed to %s but could not schedule reboot deletion: %v\n",
			aside, derr)
	} else {
		fmt.Fprintf(os.Stderr,
			"grafel uninstall: removed binary from install path; running copy %s will be deleted on next reboot\n",
			aside)
	}
	// Try an immediate removal too — succeeds if nothing holds the handle.
	_ = os.Remove(aside)
	return nil
}

// scheduleDeleteOnReboot asks Windows to delete path on the next reboot, after
// any lock on it is released. Passing a NULL destination with
// MOVEFILE_DELAY_UNTIL_REBOOT registers a pending delete in the registry.
func scheduleDeleteOnReboot(path string) error {
	from, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
}

// isAccessOrSharingError reports whether err is a Windows lock/permission
// failure of the kind raised when deleting a file backing a running process.
func isAccessOrSharingError(err error) bool {
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

// caseFold lower-cases p so path comparison on Windows (a case-insensitive
// filesystem) treats C:\Foo and c:\foo as equal.
func caseFold(p string) string { return strings.ToLower(p) }

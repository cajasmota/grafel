// removebinary.go implements self-delete-safe removal of the installed CLI
// binary for `grafel uninstall --remove-binary` (#5264).
//
// The tricky case is platform-specific: on Windows you cannot delete the file
// backing a running executable, and the uninstall command IS that executable
// deleting itself. removeBinary therefore dispatches to a build-tagged
// implementation (removeBinaryPlatform):
//
//   - Unix/other (removebinary_other.go): a plain os.Remove. Unlinking a
//     running binary is permitted on Unix, so nothing special is needed.
//   - Windows (removebinary_windows.go): try os.Remove first; if it fails with
//     a sharing/access error AND the target is the currently running exe,
//     rename it aside within the same directory (allowed for a running exe)
//     and schedule the orphan for deletion on reboot via MoveFileEx.
//
// The platform-neutral helpers (isRunningExecutable, sameFile,
// renamedAsidePath) live here so they can be unit-tested on any OS.
package install

import (
	"os"
	"path/filepath"
	"strconv"
)

// osRemove is os.Remove indirected so tests can simulate the Windows
// "access denied while running" failure on a non-Windows host.
var osRemove = os.Remove

// removeBinary removes the installed CLI binary at binPath. It returns nil when
// the binary is gone from its canonical install path afterwards — which, on
// Windows, may mean it was renamed aside and scheduled for deletion on reboot
// rather than deleted immediately (see removebinary_windows.go).
func removeBinary(binPath string) error {
	return removeBinaryPlatform(binPath)
}

// isRunningExecutable reports whether binPath refers to the same file as the
// currently running executable. Both paths are canonicalised (symlinks
// resolved, absolute) before comparison so that e.g. a relative invocation or a
// symlinked install dir still matches. On any error resolving either side it
// returns false (treat as "not the running exe" — the caller then just does a
// plain remove).
func isRunningExecutable(binPath string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	return sameFile(binPath, self)
}

// sameFile reports whether two paths resolve to the same on-disk file. It first
// tries os.SameFile (inode/volume identity, the most robust check) and falls
// back to comparing canonicalised path strings when stat fails (e.g. the file
// was already renamed). Path canonicalisation resolves symlinks and makes the
// paths absolute; on Windows callers additionally normalise case.
func sameFile(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr == nil && berr == nil {
		if os.SameFile(ai, bi) {
			return true
		}
	}
	return canonicalPath(a) == canonicalPath(b)
}

// canonicalPath returns an absolute, symlink-resolved form of p for comparison.
// It is best-effort: each step is skipped if it errors, so the function never
// fails — at worst it returns p mostly unchanged. Windows case-folding is
// applied by the windows build (see removebinary_windows.go's caseFold hook).
func canonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return caseFold(filepath.Clean(p))
}

// renamedAsidePath derives a unique sibling path used to move a running exe out
// of its canonical install location before scheduling it for delayed deletion.
// It lives next to the original (same directory, so the rename is a cheap
// directory-entry change that Windows permits even for a running exe) and
// embeds the pid to avoid colliding with a concurrent uninstall.
func renamedAsidePath(binPath string, pid int) string {
	dir := filepath.Dir(binPath)
	base := filepath.Base(binPath)
	return filepath.Join(dir, "."+base+".delete-"+strconv.Itoa(pid)+".old")
}

//go:build !windows

package groupalgo

import "os"

// atomicRename performs the temp→dest swap that finalizes an overlay write.
//
// On Unix, os.Rename over an existing destination is a single atomic syscall
// even while concurrent readers hold the destination open (open files are
// referenced by inode, not by directory entry), so a single attempt is correct
// and there is NO behavior change versus a bare os.Rename. The retrying
// variant lives in atomicrename_windows.go, where renaming over a file a
// reader still has open can transiently fail with a sharing violation.
func atomicRename(tmp, dst string) error {
	return os.Rename(tmp, dst)
}

//go:build !windows

package descriptions

import "os"

// atomicRename performs the temp→dest swap that finalizes a sidecar write.
//
// On Unix, os.Rename over an existing destination is a single atomic syscall
// even while concurrent readers hold the destination open (open files are
// referenced by inode, not by directory entry), so one attempt is correct. The
// retrying variant lives in atomicrename_windows.go, where renaming over a file
// a reader still has open can transiently fail with a sharing violation.
func atomicRename(tmp, dst string) error {
	return os.Rename(tmp, dst)
}

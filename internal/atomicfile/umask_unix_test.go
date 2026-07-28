//go:build !windows

package atomicfile_test

import (
	"syscall"
	"testing"
)

// setUmask sets the process umask and returns the previous value.
//
// This is process-global and racy by nature, which is exactly why the helper
// itself never reads it (#5978). It is safe here only because the tests in
// this package do not run in parallel with each other.
func setUmask(t *testing.T, mask int) int {
	t.Helper()
	return syscall.Umask(mask)
}

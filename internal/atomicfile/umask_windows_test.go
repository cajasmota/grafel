//go:build windows

package atomicfile_test

import "testing"

// setUmask has no meaning on Windows; the umask test is skipped there.
func setUmask(t *testing.T, mask int) int {
	t.Helper()
	t.Skip("umask is not a Windows concept")
	return 0
}

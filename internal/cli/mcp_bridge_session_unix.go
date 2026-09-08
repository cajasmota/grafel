//go:build !windows

package cli

import (
	"fmt"
	"os"
	"syscall"
)

// stdinIdentity returns a stable identifier for this process's stdin — the
// device/inode pair of the pipe the MCP client handed us. Two bridges started
// by two different clients hold two different pipes, so this separates
// concurrent sessions without naming a path, a repo or a worktree.
//
// It is best-effort: an unstattable stdin, or one whose Sys() is not a unix
// stat, returns ("", false) and the caller falls back to the process pid. A
// COLLIDING identity (both bridges on /dev/null) is harmless — the identity
// only scopes a diagnostic record's filename, never a signal.
func stdinIdentity() (string, bool) {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d-%d", uint64(st.Dev), uint64(st.Ino)), true
}

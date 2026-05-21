//go:build windows

package daemon

// chmodSocket is a no-op on Windows — the named-pipe transport already
// sets a per-owner security descriptor (SDDL "D:P(A;;GA;;;OW)") when the
// pipe is created, so no post-creation chmod is required.
func chmodSocket(_ string) error { return nil }

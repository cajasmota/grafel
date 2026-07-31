//go:build !windows

package atomicfile

// The POSIX primitives, all inert. rename(2) replaces the destination whatever
// the destination's own mode is and whoever else has it open, so there is
// nothing here to recover from — see rename.go. Keeping them constant (rather
// than #ifdef-ing the call away) means renameOver is the same code path on
// every platform and its unit tests are not Windows-only.

func renameErrRecoverable(error) bool { return false }

func destIsReadOnly(string) bool { return false }

func setDestReadOnly(string, bool) error { return nil }

//go:build !windows

package wiztui

// enableUTF8Console is a no-op on non-Windows platforms, which are already
// UTF-8 by default. It returns a no-op restore func so callers can defer it
// unconditionally (#5340).
func enableUTF8Console() func() { return func() {} }

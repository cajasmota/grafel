//go:build !unix

package safeio

import "os"

// nonBlockingOpen is plain os.Open on Windows and any other non-Unix GOOS.
//
// There is no O_NONBLOCK to ask for, and no FIFO-in-a-directory shape to
// defend against: Windows named pipes live in \\.\pipe\ and a directory walk
// does not produce them. The stat gate and the deadline in Open carry the
// contract here, which is the same split walk/gitignore_windows.go has used
// since #1781.
func nonBlockingOpen(path string) (*os.File, error) {
	return os.Open(path)
}

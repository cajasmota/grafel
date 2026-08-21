//go:build !unix

package safeio

import (
	"fmt"
	"os"
)

// nonBlockingOpen on Windows and any other non-Unix GOOS.
//
// WHAT THIS DOES AND DOES NOT GUARANTEE. There is no O_NONBLOCK to ask for
// here, so this layer cannot promise that the open RETURNS — that half of the
// contract is carried solely by the deadline in openWithDeadline, and the
// package doc states exactly what that degrades into. An earlier version of
// this file claimed the split was safe because "Windows named pipes live in
// \\.\pipe\ and a directory walk does not produce them", which is a claim about
// the shape of the tree, not about the open, and no test pinned it.
//
// What it DOES give is the other half, and it is the load-bearing one: fstat on
// the descriptor we hold rather than a second stat of the path we asked for.
// That answers "what did I actually open", and it cannot be raced, because a
// swap of the path after the open cannot change the object behind an open
// handle. It is the same layer open_unix.go gets from syscall.Fstat, reached
// through os.File.Stat, and TestDescriptorTypeGateIsUnraceable pins it on every
// GOOS including the windows-latest job.
func nonBlockingOpen(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, serr := f.Stat()
	if serr != nil {
		_ = f.Close()
		return nil, &os.PathError{Op: "fstat", Path: path, Err: serr}
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s (%s)", ErrNotRegular, path, Kind(fi.Mode()))
	}
	return f, nil
}

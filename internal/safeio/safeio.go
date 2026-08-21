// Package safeio provides opens and reads that cannot block forever on a
// non-regular file.
//
// WHY THIS EXISTS. `os.ReadFile` and `os.Open` on a FIFO block in open(2)
// until a writer appears. Nothing bounds that wait: no timeout, no error, no
// log line. A FIFO named `package.json` or `creds.go` inside a scanned tree
// therefore wedges whichever goroutine picks it up, forever, and an
// unprivileged user can plant one with a single `mkfifo` (#6416). A character
// device is the second shape of the same bug — /dev/zero opens fine and never
// reaches EOF, so the read runs until memory does.
//
// It is a shared package rather than a fourth hand-rolled copy of the same
// dance. grafel already had this pattern twice — internal/daemon/walk's
// openWithDeadline (#1729/#1773, written for macOS fsevents kernel stalls) and
// internal/mcp/read_source_unix.go — and the #6416 review found two MORE
// unguarded call sites (internal/install/detect, internal/secrets), which is
// what a copied pattern always produces. New blocking-open sites should call
// this package.
//
// THE GUARD IS TWO-LAYERED, and both layers are load-bearing:
//
//  1. A stat gate. Only a regular file is opened at all. This is the cheap,
//     portable layer and it is what makes the common case (a FIFO sitting in
//     the tree) impossible rather than merely bounded.
//
//  2. A non-blocking open, on Unix. The stat gate has an inherent TOCTOU
//     residual: a regular file swapped for a FIFO between the stat and the
//     open still hangs. O_NONBLOCK is what actually closes that window —
//     POSIX guarantees open(2) with O_NONBLOCK returns rather than waits.
//     Windows has no O_NONBLOCK and no FIFO-in-a-directory shape to defend
//     against; the deadline and the stat gate carry it there.
//
// A 64-slot semaphore bounds how many outstanding opens can be in flight, so a
// platform that fails to honour O_NONBLOCK degrades into bounded leakage
// instead of unbounded. This mirrors walk.openWithDeadline exactly.
package safeio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ErrNotRegular is returned when the path exists but is not a regular file —
// a named pipe, device, socket, directory, or a symlink to one of those.
//
// It is a distinct error, not a generic failure, because every caller so far
// wants to REPORT the skip rather than swallow it: a source-looking file that
// produced nothing and was reported nowhere is the diagnosis cost #6338
// measured.
var ErrNotRegular = errors.New("safeio: not a regular file")

// ErrWouldBlock is returned when the open would have blocked — the TOCTOU case
// where the path became a FIFO after the stat gate passed.
var ErrWouldBlock = errors.New("safeio: open would block")

// DefaultTimeout bounds the open. It is a backstop for a platform that does
// not honour O_NONBLOCK, NOT the mechanism: the stat gate and O_NONBLOCK are
// what make the hang absent, and a timeout alone would only make it
// intermittent while firing on slow-but-legitimate reads.
const DefaultTimeout = 5 * time.Second

// openSlots bounds concurrently-outstanding open goroutines, so a kernel that
// ignores O_NONBLOCK leaks at most this many rather than one per scanned file.
// 64 is the value walk.openWithDeadline has used since #1773.
var openSlots = make(chan struct{}, 64)

// SymlinkPolicy says what a symlink resolves to.
//
// It is an explicit parameter because the two existing call sites disagree for
// good reasons, and a silent default would quietly pick one.
type SymlinkPolicy int

const (
	// FollowSymlinks resolves with os.Stat and judges the TARGET. A symlink to
	// a regular file is accepted; a symlink to a FIFO is refused. This is what
	// a file scanner wants: the walker mints a file entity for a
	// symlink-to-file today, so refusing them would delete legitimate coverage
	// rather than being merely conservative.
	FollowSymlinks SymlinkPolicy = iota
	// RejectSymlinks resolves with os.Lstat, so a symlink of any kind is
	// refused. For callers that must not be redirected out of a tree.
	RejectSymlinks
)

// Stat applies the type gate and returns the FileInfo the decision was made
// on. It never opens the path, so — unlike os.Open — it cannot block on a
// FIFO: stat(2) does not wait for a writer.
func Stat(path string, policy SymlinkPolicy) (os.FileInfo, error) {
	var (
		fi  os.FileInfo
		err error
	)
	if policy == RejectSymlinks {
		fi, err = os.Lstat(path)
	} else {
		fi, err = os.Stat(path)
	}
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s (%s)", ErrNotRegular, path, Kind(fi.Mode()))
	}
	return fi, nil
}

// Kind names an entry type for a skip report. The names are human-facing: the
// point of reporting the skip is that a reader can tell WHY a file vanished,
// and "named-pipe" says that where a raw mode bitmask does not.
func Kind(mode os.FileMode) string {
	switch {
	case mode.IsRegular():
		return "regular"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeNamedPipe != 0:
		return "named-pipe"
	case mode&os.ModeDevice != 0:
		return "device"
	case mode&os.ModeSocket != 0:
		return "socket"
	default:
		// Doors on Solaris/illumos, and anything a future GOOS reports as
		// ModeIrregular.
		return "other"
	}
}

// Open opens path for reading, or returns ErrNotRegular / ErrWouldBlock rather
// than blocking. The caller closes the returned file.
func Open(path string, policy SymlinkPolicy) (*os.File, error) {
	if _, err := Stat(path, policy); err != nil {
		return nil, err
	}

	// Bail rather than pile up if the process is already saturated with
	// outstanding opens — that state means something upstream is already
	// wedged, and adding to it makes the diagnosis harder, not the work
	// faster.
	select {
	case openSlots <- struct{}{}:
	default:
		return nil, ErrWouldBlock
	}

	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := nonBlockingOpen(path)
		ch <- result{f: f, err: err}
		<-openSlots
	}()

	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(DefaultTimeout):
		// Defensive only: a non-blocking open should never reach here. The
		// semaphore slot is released by the worker if it ever unblocks.
		return nil, ErrWouldBlock
	}
}

// ReadFile is the os.ReadFile replacement. maxBytes caps how much is read
// (0 = unlimited); it exists because a character device never reaches EOF, so
// "it will hit EOF eventually" is not a bound.
func ReadFile(path string, policy SymlinkPolicy, maxBytes int64) ([]byte, error) {
	f, err := Open(path, policy)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	if maxBytes > 0 {
		r = io.LimitReader(f, maxBytes)
	}
	return io.ReadAll(r)
}

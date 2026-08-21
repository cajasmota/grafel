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
//  2. A type check on the OPEN DESCRIPTOR, on every GOOS. The stat gate has an
//     inherent TOCTOU residual: a regular file swapped for a FIFO between the
//     stat and the open is not caught by the stat. Asking the descriptor —
//     fstat(2) on Unix, os.File.Stat elsewhere — closes that window, because a
//     swap of the path cannot change the object behind a handle we already
//     hold. This layer is portable and it is the load-bearing one.
//
//     Unix additionally opens with O_NONBLOCK, which is a guarantee about a
//     different thing: that the open RETURNS. POSIX promises that; Windows
//     offers no equivalent, so on non-Unix the "it returns" half rests on the
//     deadline alone. That is a real difference in strength and it is stated
//     here rather than papered over.
//
// WHAT THE DEADLINE ACTUALLY BUYS, stated honestly, because the first version
// of this comment claimed a property the code did not have. An open that
// genuinely never returns cannot be cancelled: there is no portable way to
// interrupt a thread parked in open(2). So the deadline does not rescue the
// worker — it rescues the CALLER. The worker is abandoned, it keeps its
// semaphore slot, and it is never counted as returned. That is the whole of
// the guarantee:
//
//   - The caller of Open always returns within DefaultTimeout, or twice it in
//     the worst case where the slot wait also has to time out.
//   - At most cap(openSlots) workers can be abandoned before the package stops
//     being useful. Past that point every call waits DefaultTimeout and returns
//     ErrWouldBlock. That is a real terminal state, not a scare quote — it is
//     just a bounded and REPORTED one. ErrWouldBlock is a distinct error for
//     exactly this reason: a caller that maps it to "nothing found" turns a
//     bounded degradation back into a silent one, which is the shape #6338 was
//     about.
//
// Reaching that state needs cap(openSlots) opens that never return at all. On
// Unix, O_NONBLOCK means a FIFO is not one of them. On Windows os.Open has no
// such flag, so the "it returns" half rests on the deadline alone there; that
// is measured, not assumed — flipping the build tag so open_other.go compiles
// on darwin, nonBlockingOpen does block on a real FIFO for the full deadline.
//
// The semaphore acquire is a BOUNDED WAIT, not a fail-fast. An earlier draft
// used a non-blocking select with a default arm, which refused 335 of 400
// perfectly ordinary regular files under concurrency — a limiter that refuses
// work is not a limiter, and because internal/secrets maps an open error to
// "no findings", the user-visible result was a scanner reporting a tree clean
// after reading 16% of it.
package safeio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
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

// openSlots bounds concurrently-outstanding open workers, so a kernel that
// parks in open(2) regardless of O_NONBLOCK leaks at most this many rather than
// one per scanned file. 64 is the value walk.openWithDeadline has used since
// #1773.
//
// Slots are returned by whichever side finishes first — the caller on the fast
// path, so a completed open frees its slot BEFORE Open returns rather than at
// some later scheduling point, and the worker if the caller has already given
// up. Only a worker that never returns at all holds one forever, which is
// exactly the leak the bound is for.
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
	return openWithDeadline(path, nonBlockingOpen, DefaultTimeout)
}

// openWithDeadline runs open in a worker and bounds the CALLER's wait. It takes
// the opener as a parameter for one reason that is not testability theatre: it
// is the only way to exercise this layer with an open that genuinely blocks.
// A real blocking open cannot be conjured on every GOOS — Windows has no
// mkfifo — so the machinery that has to work on Windows would otherwise be
// asserted-safe by comment only, which is what .github/workflows/test.yml's
// windows-latest job was silently accepting.
func openWithDeadline(path string, open func(string) (*os.File, error), timeout time.Duration) (*os.File, error) {
	// A bounded wait, not a fail-fast. Under saturation a slot frees as soon as
	// any concurrent open completes, and a legitimate open completes in
	// microseconds; only genuinely-abandoned workers hold slots long enough for
	// this arm to fire.
	timer := time.NewTimer(timeout)
	select {
	case openSlots <- struct{}{}:
		timer.Stop()
	case <-timer.C:
		return nil, ErrWouldBlock
	}

	var (
		releaseOnce sync.Once
		mu          sync.Mutex
		abandoned   bool
	)
	release := func() { releaseOnce.Do(func() { <-openSlots }) }

	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := open(path)
		mu.Lock()
		gaveUp := abandoned
		mu.Unlock()
		if gaveUp {
			// Nobody is left holding this descriptor, so nobody will close it.
			if f != nil {
				_ = f.Close()
			}
			release()
			return
		}
		ch <- result{f: f, err: err}
		release()
	}()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	select {
	case r := <-ch:
		// Release here rather than leaving it to the worker: the worker's
		// release races with the caller's next Open, and that race is what made
		// the fail-fast draft refuse regular files.
		release()
		return r.f, r.err
	case <-deadline.C:
		mu.Lock()
		abandoned = true
		mu.Unlock()
		// The slot stays held. The worker is parked in open(2) and cannot be
		// interrupted; it will return the slot if it ever unblocks, and hold it
		// forever if it does not. See the package doc.
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

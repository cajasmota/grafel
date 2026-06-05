//go:build darwin || linux

package mcp

// read_source_unix.go — non-blocking open(2) defense for readSourceWindow.
//
// #1773: on macOS the daemon holds fsnotify/fsevents watchers on every indexed
// source tree. A raw os.Open on a path in such a tree can block indefinitely
// inside the kernel, causing archigraph_get_source to always time out at
// exactly 5.000s. The fix applies the same three-layer defense used in
// internal/daemon/walk/gitignore.go (post-#1729):
//
//  1. os.Lstat short-circuit — bail immediately for non-regular files.
//  2. syscall.Open with O_NONBLOCK — returns without blocking even during an
//     fsevents kernel stall; O_NONBLOCK is cleared after a successful open so
//     that subsequent Read(2) calls behave normally.
//  3. 64-slot global semaphore (getSourceSem) — caps the number of
//     concurrently-outstanding open goroutines to prevent accumulation if the
//     kernel ignores O_NONBLOCK on some path type.
//
// The 5s deadline in handleGetNodeSource is preserved as a true safety net; in
// practice it will rarely fire once the non-blocking open path is in effect.
//
// The plain-os.Open counterparts of openSourceFile and readSourceWindow for
// non-unix platforms live in read_source_other.go (//go:build !darwin && !linux).

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

// getSourceSem bounds the number of concurrently-outstanding open workers
// inside readSourceWindow. Mirrors openSlotSem in internal/daemon/walk/gitignore.go.
// 64 slots is generous for the MCP request rate; real requests complete in
// microseconds so the semaphore is only load-bearing during genuine kernel stalls.
var getSourceSem = make(chan struct{}, 64)

// errGetSourceStall is the sentinel returned when readSourceWindow detects an
// fsevents kernel stall (O_NONBLOCK open would block, semaphore saturated, or
// the per-call deadline fires before the open returns).
var errGetSourceStall = errors.New("readSourceWindow: fsevents kernel stall detected (O_NONBLOCK open would block)")

// openSourceFile opens path for reading using a non-blocking open(2).
// It applies the three-layer defense against macOS fsevents kernel stalls.
// The returned *os.File has O_NONBLOCK cleared and is ready for buffered reads.
// The caller must Close it.
func openSourceFile(path string) (*os.File, error) {
	// Layer 1: lstat short-circuit. Non-existent or non-regular paths fail
	// immediately without touching the kernel's watched-inode path.
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.ENOTSUP}
	}

	// Layer 2: semaphore — bail early if already saturated.
	select {
	case getSourceSem <- struct{}{}:
	default:
		return nil, errGetSourceStall
	}

	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		// Layer 3: non-blocking open(2). POSIX guarantees this returns
		// without blocking; on macOS fsevents stalls this is the actual fix.
		fd, oerr := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if oerr != nil {
			if errors.Is(oerr, syscall.EWOULDBLOCK) || errors.Is(oerr, syscall.EAGAIN) {
				ch <- result{err: errGetSourceStall}
				<-getSourceSem
				return
			}
			ch <- result{err: &os.PathError{Op: "open", Path: path, Err: oerr}}
			<-getSourceSem
			return
		}

		// Clear O_NONBLOCK so buffered Read(2) calls behave normally.
		// Failure is non-fatal for regular files, but do it for hygiene.
		if flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0); errno == 0 && (int(flags)&syscall.O_NONBLOCK) != 0 {
			_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFL), flags&^uintptr(syscall.O_NONBLOCK))
		}

		ch <- result{f: os.NewFile(uintptr(fd), path)}
		<-getSourceSem
	}()

	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(5 * time.Second):
		// Defensive: the goroutine will eventually release its semaphore slot
		// when the kernel unblocks, preventing unbounded accumulation.
		return nil, errGetSourceStall
	}
}

// readSourceWindow opens path using the non-blocking open defense, then scans
// lines [start,end] (1-indexed inclusive) and returns the formatted text.
//
// Split out so handleGetNodeSource can run the call on a worker goroutine
// bounded by a context deadline (#1678, #1773).
func readSourceWindow(path string, start, end int) (string, error) {
	f, err := openSourceFile(path)
	if err != nil {
		if errors.Is(err, errGetSourceStall) {
			return "", fmt.Errorf("readSourceWindow: %w (fsevents stall on %s)", err, path)
		}
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	var b strings.Builder
	line := 0
	for scanner.Scan() {
		line++
		if line < start {
			continue
		}
		if line > end {
			break
		}
		b.WriteString(fmt.Sprintf("%5d  %s\n", line, scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}

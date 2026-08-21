//go:build unix

package safeio

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestOpenDoesNotBlockOnFifo is the liveness assertion #6416 is about. Plain
// os.Open on this same path waits for a writer forever; deliberately not run
// here, because it would hang the suite rather than report.
func TestOpenDoesNotBlockOnFifo(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "creds.go")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	type res struct{ err error }
	ch := make(chan res, 1)
	go func() {
		f, err := Open(fifo, FollowSymlinks)
		if f != nil {
			f.Close()
		}
		ch <- res{err}
	}()
	select {
	case r := <-ch:
		if !errors.Is(r.err, ErrNotRegular) {
			t.Errorf("Open on a fifo = %v, want ErrNotRegular", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open blocked on a fifo")
	}
}

// TestReadFileDoesNotBlockOnFifoBehindASymlink covers the shape the stat gate
// would miss if it used Lstat: WalkDir-style callers see ModeSymlink for a link
// to a regular file and a link to a FIFO alike, so the gate has to judge the
// target.
func TestReadFileDoesNotBlockOnFifoBehindASymlink(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "real_fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}
	link := filepath.Join(root, "package.json")
	if err := os.Symlink(fifo, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ReadFile(link, FollowSymlinks, 1<<20)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Errorf("ReadFile through a symlink to a fifo = %v, want ErrNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFile blocked on a symlink to a fifo")
	}
}

// TestNonBlockingOpenIsTheTOCTOULayer drives the low-level open DIRECTLY,
// bypassing the stat gate, which is the only way to exercise the residual the
// gate cannot cover: a regular file swapped for a FIFO between the stat and
// the open.
//
// It also pins the POSIX detail that broke the first draft. O_NONBLOCK does
// not make a FIFO fail to open — the read-end open SUCCEEDS immediately — so
// a version that trusted the errno alone handed back a descriptor whose first
// Read would block. The fstat-on-the-descriptor is what actually answers "what
// did I open", and it is unraceable because it asks the fd, not the path.
func TestNonBlockingOpenIsTheTOCTOULayer(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "swapped.go")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		f, err := nonBlockingOpen(fifo)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Errorf("nonBlockingOpen on a fifo = %v, want ErrNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nonBlockingOpen blocked; this layer is what closes the stat/open race and it is not working")
	}
}

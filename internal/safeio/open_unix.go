//go:build unix

package safeio

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// nonBlockingOpen opens path with O_NONBLOCK and then re-checks the type on
// the OPEN DESCRIPTOR with fstat(2). Both steps are needed, and the reason is
// a POSIX detail that is easy to get wrong:
//
// O_NONBLOCK does NOT make a FIFO fail to open. Opening the read end of a FIFO
// with O_NONBLOCK returns IMMEDIATELY AND SUCCESSFULLY, writer or no writer —
// that is precisely what the flag is for. Only the subsequent read(2) would
// block, and this function used to clear O_NONBLOCK before handing the file
// back, which moved the hang from open to the first Read instead of removing
// it. The first draft of this file asserted ErrWouldBlock here and the test
// caught it: the fifo opened cleanly.
//
// So the flag buys the guarantee that the OPEN returns, and fstat on the
// resulting fd buys the answer to "what did I actually open". Because fstat
// asks the descriptor rather than the path, it cannot be raced: whatever the
// path was swapped to between safeio.Stat and here, this is the object the
// caller would read. That is the residual the stat gate inherently has and the
// reason this layer exists at all.
//
// O_NONBLOCK is cleared only for a descriptor that survived the fstat, i.e. a
// regular file, where non-blocking semantics are meaningless and clearing is
// pure hygiene.
func nonBlockingOpen(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		// ENXIO is what a write-only FIFO open returns with O_NONBLOCK, and
		// EWOULDBLOCK/EAGAIN is the generic "would have waited" answer.
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENXIO) {
			return nil, ErrWouldBlock
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	var st syscall.Stat_t
	if ferr := syscall.Fstat(fd, &st); ferr != nil {
		_ = syscall.Close(fd)
		return nil, &os.PathError{Op: "fstat", Path: path, Err: ferr}
	}
	if mode := fileModeOf(uint32(st.Mode)); !mode.IsRegular() {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("%w: %s (%s)", ErrNotRegular, path, Kind(mode))
	}

	if flags, ferr := fcntlGetFl(fd); ferr == nil && flags&syscall.O_NONBLOCK != 0 {
		_ = fcntlSetFl(fd, flags&^syscall.O_NONBLOCK)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// fileModeOf converts a raw st_mode (uint16 on darwin, uint32 on linux —
// hence the caller-side conversion) into the os.FileMode type bits, so Kind
// and IsRegular can be reused rather than re-deriving S_IFMT comparisons.
func fileModeOf(raw uint32) os.FileMode {
	var m os.FileMode
	switch raw & syscall.S_IFMT {
	case syscall.S_IFREG:
		// zero type bits = regular
	case syscall.S_IFDIR:
		m |= os.ModeDir
	case syscall.S_IFIFO:
		m |= os.ModeNamedPipe
	case syscall.S_IFLNK:
		m |= os.ModeSymlink
	case syscall.S_IFSOCK:
		m |= os.ModeSocket
	case syscall.S_IFCHR:
		m |= os.ModeDevice | os.ModeCharDevice
	case syscall.S_IFBLK:
		m |= os.ModeDevice
	default:
		m |= os.ModeIrregular
	}
	return m
}

func fcntlGetFl(fd int) (int, error) {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		return 0, errno
	}
	return int(flags), nil
}

func fcntlSetFl(fd, flags int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFL), uintptr(flags))
	if errno != 0 {
		return errno
	}
	return nil
}

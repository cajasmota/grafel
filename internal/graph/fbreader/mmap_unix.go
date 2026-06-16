//go:build darwin || linux

package fbreader

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// mmapRegion is a read-only memory mapping of a file. The data slice is a
// page-cache-backed view: reading from it faults pages in lazily and costs
// no heap. The mapping stays valid until unmap is called; FlatBuffer
// accessors read lazily against data, so it must outlive every read.
type mmapRegion struct {
	data []byte
}

// mmapOpen memory-maps path read-only and returns a contiguous []byte view
// of the whole file. No bytes are copied to the heap — the returned slice
// aliases the page cache directly.
func mmapOpen(path string) (*mmapRegion, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// We can close the fd immediately: on Unix the mapping keeps its own
	// reference to the underlying file, so the region stays valid after the
	// descriptor is gone.
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	if size == 0 {
		return nil, fmt.Errorf("fbreader: %s is empty", path)
	}
	if int64(int(size)) != size {
		return nil, fmt.Errorf("fbreader: %s too large to mmap on this platform (%d bytes)", path, size)
	}

	data, err := unix.Mmap(int(f.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("fbreader: mmap %s: %w", path, err)
	}
	return &mmapRegion{data: data}, nil
}

// bytes returns the contiguous file view. The slice is valid only until
// unmap is called.
func (m *mmapRegion) bytes() []byte { return m.data }

// unmap releases the mapping. After it returns, the slice from bytes() must
// not be touched.
func (m *mmapRegion) unmap() error {
	if m == nil || m.data == nil {
		return nil
	}
	err := unix.Munmap(m.data)
	m.data = nil
	return err
}

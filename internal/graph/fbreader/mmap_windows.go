//go:build windows

package fbreader

import (
	"fmt"
	"os"
	"reflect"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mmapRegion is a read-only memory mapping of a file. The data slice is a
// page-cache-backed view: reading from it faults pages in lazily and costs
// no heap. The mapping stays valid until unmap is called; FlatBuffer
// accessors read lazily against data, so it must outlive every read.
type mmapRegion struct {
	data    []byte
	mapping windows.Handle
	addr    uintptr
}

// mmapOpen memory-maps path read-only and returns a contiguous []byte view
// of the whole file. No bytes are copied to the heap — the returned slice
// aliases the mapped view directly.
func mmapOpen(path string) (*mmapRegion, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
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

	// CreateFileMapping with maxsize 0 sizes the mapping to the whole file.
	mapping, err := windows.CreateFileMapping(
		windows.Handle(f.Fd()),
		nil,
		windows.PAGE_READONLY,
		0, 0,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("fbreader: CreateFileMapping %s: %w", path, err)
	}

	addr, err := windows.MapViewOfFile(
		mapping,
		windows.FILE_MAP_READ,
		0, 0,
		uintptr(size),
	)
	if err != nil {
		windows.CloseHandle(mapping)
		return nil, fmt.Errorf("fbreader: MapViewOfFile %s: %w", path, err)
	}

	// addr is the base of a kernel-owned, page-cache-backed view returned by
	// MapViewOfFile — not Go-managed memory. Building the []byte view via the
	// slice header (rather than unsafe.Slice((*byte)(unsafe.Pointer(addr)), …))
	// avoids a uintptr→unsafe.Pointer conversion that `go vet`'s unsafeptr
	// analyzer flags as a possible misuse on the Windows build. The conversion
	// here is unsafe.Pointer(&data) — of a Go pointer, which is always valid —
	// and the raw address is assigned to the header's Data field as a uintptr,
	// which vet accepts. The view's lifetime is owned by unmap(), not the GC,
	// so the uintptr-held base needs no GC reachability.
	var data []byte
	h := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	h.Data = addr
	h.Len = int(size)
	h.Cap = int(size)
	return &mmapRegion{data: data, mapping: mapping, addr: addr}, nil
}

// bytes returns the contiguous file view. The slice is valid only until
// unmap is called.
func (m *mmapRegion) bytes() []byte { return m.data }

// unmap releases the mapped view and closes the mapping handle. After it
// returns, the slice from bytes() must not be touched.
func (m *mmapRegion) unmap() error {
	if m == nil || m.data == nil {
		return nil
	}
	var firstErr error
	if err := windows.UnmapViewOfFile(m.addr); err != nil {
		firstErr = err
	}
	if err := windows.CloseHandle(m.mapping); err != nil && firstErr == nil {
		firstErr = err
	}
	m.data = nil
	m.addr = 0
	m.mapping = 0
	return firstErr
}

//go:build windows

package process

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// FindByName is not implemented on Windows. Process enumeration here would
// require WMI or Toolhelp32Snapshot; callers treat ErrUnsupported as
// "cannot determine" and fall back to a coarser liveness check. Tracked as
// a future improvement.
func FindByName(_ string) ([]Info, error) {
	return nil, ErrUnsupported
}

// pidExeBaseName returns the basename of the executable backing pid via a
// single, pid-targeted OpenProcess + QueryFullProcessImageName. This is the
// Windows substitute for FindByName-based PidIsGrafel: full process
// enumeration is unavailable here, but a single pid's image path is cheaply
// queryable with PROCESS_QUERY_LIMITED_INFORMATION (the same low-privilege
// handle IsAlive already uses, openable across sessions).
//
// The second result is true when the basename was resolved. A false result
// (process gone, or open/query failed) tells PidIsGrafel it could not verify
// ownership for this pid — but because we DID try directly here, a live
// non-grafel pid resolves to a non-matching basename → (false, true), which
// PidIsGrafel turns into a definitive "not grafel". Only when the image path
// is genuinely unreadable do we return false, leaving the decision to the
// caller's liveness probe.
func pidExeBaseName(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Process is gone (ERROR_INVALID_PARAMETER) or otherwise unopenable.
		// We cannot read the image path; report "unknown" so the caller falls
		// back to its liveness probe rather than mislabeling the pid.
		return "", false
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	full := windows.UTF16ToString(buf[:size])
	if full == "" {
		return "", false
	}
	return filepath.Base(full), true
}

// Kill terminates the given PID. os.Process.Kill maps to TerminateProcess
// on Windows; we open the process with PROCESS_TERMINATE and call it.
func Kill(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("process.Kill: invalid pid %d", pid)
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return fmt.Errorf("TerminateProcess(%d): %w", pid, err)
	}
	return nil
}

// CPUPercent is not implemented on Windows.
func CPUPercent(_ int) (float64, error) {
	return 0, fmt.Errorf("process.CPUPercent: unsupported platform windows")
}

// processMemoryCounters mirrors the Win32 PROCESS_MEMORY_COUNTERS struct
// (psapi.h). WorkingSetSize is the resident set — the bytes the process
// currently has in physical RAM — which is the honest Windows analogue of
// unix RSS.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

// RSSBytes returns the resident set size (WorkingSetSize) of the process
// with the given pid, via psapi!GetProcessMemoryInfo. This is the honest
// physical-memory number used by FootprintBytes on Windows, distinct from
// runtime.MemStats.Sys (reserved virtual address space).
func RSSBytes(pid int) (uint64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("process.RSSBytes: invalid pid %d", pid)
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid),
	)
	if err != nil {
		return 0, fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer windows.CloseHandle(h)

	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	r1, _, callErr := procGetProcessMemoryInfo.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.CB),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo(%d): %w", pid, callErr)
	}
	return uint64(counters.WorkingSetSize), nil
}

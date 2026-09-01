//go:build windows

package testsupport

// tempdirdiag_windows.go — the "who is holding it?" half of the #6512
// diagnostic.
//
// # Why the Restart Manager and not handle64.exe
//
// #6512 names `handle64.exe -p`. That is the right instrument on a developer
// box, but handle64.exe ships in Sysinternals Suite and is NOT part of the
// GitHub windows-latest runner image, so a diagnostic that depends on it would
// silently print nothing on the one machine where it matters. `openfiles.exe`
// is worse: it exists in-box but only reports anything after
// `openfiles /local on` plus a REBOOT, which CI cannot do. PowerShell has no
// built-in handle enumerator.
//
// The Restart Manager API (rstrtmgr.dll: RmStartSession / RmRegisterResources
// / RmGetList / RmEndSession) is in-box on every supported Windows, needs no
// privilege elevation and no reboot, and answers exactly the question asked:
// "which processes currently hold this path open?" It is what MSI and Windows
// Update use to decide whom to ask to close a file. No new module dependency
// is needed — golang.org/x/sys is already a direct requirement of this module.
//
// handle64.exe is still used when it happens to be on PATH, because on a
// developer machine it reports the handle TYPE and NAME, which Restart Manager
// does not. Its absence is reported explicitly rather than passed over, so a CI
// reader is never left wondering whether the tool ran and found nothing or
// never ran at all.
//
// # The known limitation, stated up front
//
// Restart Manager is file-oriented. A process holding a DIRECTORY handle open
// — which is one of the live hypotheses for #6512, since `os.ReadDir` holds
// one — may not be reported by RmGetList at all. That is why this file also
// probes each residual path with a zero-share CreateFile: the resulting Win32
// error separates ERROR_SHARING_VIOLATION (someone else has it open) from
// ERROR_ACCESS_DENIED (the classic delete-pending signature) from success
// (nothing holds it any more, so the block was transient). Those three
// outcomes are diagnostic even when RmGetList returns an empty list.

import (
	"fmt"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// CCH_RM_SESSION_KEY is sizeof(GUID)*2; the buffer needs one more for the
	// terminating NUL.
	rmSessionKeyLen = 32 + 1
	// Bound on how many holders we ask RmGetList to describe. More than a
	// handful would not narrow anything.
	rmMaxProcInfo = 32
	// Bound on how many paths we register, so a large residue cannot make the
	// diagnostic itself slow.
	rmMaxPaths = 32
)

// rmUniqueProcess mirrors RM_UNIQUE_PROCESS (DWORD + FILETIME = 12 bytes).
type rmUniqueProcess struct {
	ProcessID        uint32
	ProcessStartTime windows.Filetime
}

// rmProcessInfo mirrors RM_PROCESS_INFO. Field sizes come from
// CCH_RM_MAX_APP_NAME (255) and CCH_RM_MAX_SVC_NAME (63), each plus a NUL.
// Total is 12 + 512 + 128 + 4*4 = 668 bytes at 4-byte alignment, which is what
// Go lays this struct out as.
type rmProcessInfo struct {
	Process          rmUniqueProcess
	AppName          [256]uint16
	ServiceShortName [64]uint16
	ApplicationType  uint32
	AppStatus        uint32
	TSSessionID      uint32
	Restartable      int32
}

var (
	modRstrtmgr             = windows.NewLazySystemDLL("rstrtmgr.dll")
	procRmStartSession      = modRstrtmgr.NewProc("RmStartSession")
	procRmRegisterResources = modRstrtmgr.NewProc("RmRegisterResources")
	procRmGetList           = modRstrtmgr.NewProc("RmGetList")
	procRmEndSession        = modRstrtmgr.NewProc("RmEndSession")
)

// describeHolders reports, for the residual paths of a failed TempDir
// cleanup, which processes hold them open and how each path responds to an
// exclusive open. It never returns an error: a diagnostic that can fail is a
// diagnostic that stops reporting on the day it is needed, so every failure
// is rendered into the returned text instead.
func describeHolders(paths []string) string {
	var b strings.Builder
	b.WriteString("handle holders:\n")

	if len(paths) > rmMaxPaths {
		fmt.Fprintf(&b, "  (registering the first %d of %d paths)\n", rmMaxPaths, len(paths))
		paths = paths[:rmMaxPaths]
	}

	b.WriteString(rmHolders(paths))
	b.WriteString(exclusiveOpenProbe(paths))
	b.WriteString(handle64Report(paths))
	return b.String()
}

// rmHolders asks the Restart Manager which processes hold paths open.
func rmHolders(paths []string) string {
	var b strings.Builder

	var session uint32
	key := make([]uint16, rmSessionKeyLen)
	r, _, _ := procRmStartSession.Call(
		uintptr(unsafe.Pointer(&session)),
		0,
		uintptr(unsafe.Pointer(&key[0])),
	)
	if r != 0 {
		fmt.Fprintf(&b, "  RestartManager: RmStartSession failed (win32 error %d)\n", r)
		return b.String()
	}
	defer procRmEndSession.Call(uintptr(session)) //nolint:errcheck // diagnostic teardown

	// RmRegisterResources takes an array of *uint16. utf16Ptrs keeps the
	// backing slices alive for the duration of the call.
	ptrs := make([]*uint16, 0, len(paths))
	for _, p := range paths {
		u, err := windows.UTF16PtrFromString(p)
		if err != nil {
			fmt.Fprintf(&b, "  RestartManager: cannot encode %s: %v\n", p, err)
			continue
		}
		ptrs = append(ptrs, u)
	}
	if len(ptrs) == 0 {
		b.WriteString("  RestartManager: no registrable paths\n")
		return b.String()
	}

	r, _, _ = procRmRegisterResources.Call(
		uintptr(session),
		uintptr(len(ptrs)),
		uintptr(unsafe.Pointer(&ptrs[0])),
		0, 0, // nApplications, rgApplications
		0, 0, // nServices, rgsServiceNames
	)
	if r != 0 {
		fmt.Fprintf(&b, "  RestartManager: RmRegisterResources failed (win32 error %d)\n", r)
		return b.String()
	}

	var needed uint32
	count := uint32(rmMaxProcInfo)
	infos := make([]rmProcessInfo, rmMaxProcInfo)
	var rebootReasons uint32
	r, _, _ = procRmGetList.Call(
		uintptr(session),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&infos[0])),
		uintptr(unsafe.Pointer(&rebootReasons)),
	)
	// ERROR_MORE_DATA (234) means the buffer was too small; the first `count`
	// entries are still valid and are worth printing.
	if r != 0 && r != uintptr(windows.ERROR_MORE_DATA) {
		fmt.Fprintf(&b, "  RestartManager: RmGetList failed (win32 error %d)\n", r)
		return b.String()
	}
	if needed == 0 {
		b.WriteString("  RestartManager: NO process reported holding any residual path.\n")
		b.WriteString("    (RmGetList is file-oriented; a process holding only a DIRECTORY\n")
		b.WriteString("     handle — e.g. one sitting in os.ReadDir — may not appear here.\n")
		b.WriteString("     Read the exclusive-open probe below before concluding.)\n")
		return b.String()
	}

	fmt.Fprintf(&b, "  RestartManager: %d holder(s) reported (%d described)\n", needed, count)
	for i := uint32(0); i < count && int(i) < len(infos); i++ {
		pi := infos[i]
		fmt.Fprintf(&b, "    pid=%d app=%q service=%q type=%d status=%d session=%d restartable=%t\n",
			pi.Process.ProcessID,
			windows.UTF16ToString(pi.AppName[:]),
			windows.UTF16ToString(pi.ServiceShortName[:]),
			pi.ApplicationType, pi.AppStatus, pi.TSSessionID,
			pi.Restartable != 0,
		)
	}
	return b.String()
}

// exclusiveOpenProbe opens each residual path with dwShareMode=0. The Win32
// error is the finding:
//
//	success                    → nothing holds it now; the block was transient
//	ERROR_SHARING_VIOLATION    → another handle is open right now
//	ERROR_ACCESS_DENIED        → delete-pending, the #6512 signature
func exclusiveOpenProbe(paths []string) string {
	var b strings.Builder
	b.WriteString("exclusive-open probe (share mode 0):\n")
	for _, p := range paths {
		u, err := windows.UTF16PtrFromString(p)
		if err != nil {
			fmt.Fprintf(&b, "  %s: cannot encode: %v\n", p, err)
			continue
		}
		h, err := windows.CreateFile(
			u,
			windows.GENERIC_READ,
			0, // share with nobody
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS, // required to open a directory
			0,
		)
		if err != nil {
			fmt.Fprintf(&b, "  BLOCKED %s: %v\n", p, err)
			continue
		}
		windows.CloseHandle(h) //nolint:errcheck // diagnostic probe
		fmt.Fprintf(&b, "  open-ok %s (nothing holds it at probe time)\n", p)
	}
	return b.String()
}

// handle64Report runs Sysinternals handle64.exe when it is on PATH. It is not
// expected to be present on a GitHub runner; its absence is stated so the
// reader knows the tool did not merely find nothing.
func handle64Report(paths []string) string {
	exe, err := exec.LookPath("handle64.exe")
	if err != nil {
		return "handle64.exe: not on PATH (expected on a GitHub runner — Sysinternals is not in the image); " +
			"RestartManager + the exclusive-open probe above are the in-box substitutes\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "handle64.exe: found at %s\n", exe)
	for _, p := range paths {
		out, err := exec.Command(exe, "-nobanner", "-accepteula", "-u", p).CombinedOutput()
		fmt.Fprintf(&b, "  --- %s (err=%v)\n", p, err)
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(line))
		}
	}
	return b.String()
}

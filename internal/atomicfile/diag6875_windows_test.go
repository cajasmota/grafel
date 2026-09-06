//go:build windows

// diag6875_windows_test.go — THROWAWAY instrumentation for #6875.
//
// REMOVE BEFORE ANY FINAL COMMIT. Skipped unless GRAFEL_DIAG_6875=1.
//
// It answers three questions the CI failure cannot:
//
//  1. What is the ACTUAL attempts-to-success distribution for the rename under
//     the test's 8-way concurrency? (budget here is absurd, per the issue's
//     "point the budget at an absurd value once and read the distribution")
//  2. WHO holds the destination at the moment of denial — the Restart Manager
//     is asked live, inside the retry loop, while contention is still running.
//  3. Does a SINGLE writer doing the same number of writes to the same path in
//     the same directory ever get denied at all? That is the control that
//     separates "our own concurrent writers collide" (H1) from "an external
//     handle — Defender / the indexer — touches a freshly created file" (H2).
//     Arm B has identical external exposure and zero self-contention.
package atomicfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// diagBudget is deliberately absurd: 2000 × 5ms = 10s, ~48x the production
// budget. We want the distribution, not a pass/fail.
const (
	diagBudget    = 2000
	diagDelay     = 5 * time.Millisecond
	prodBudget    = 41 // 1 initial + 40 retries, the number CI prints
	maxLiveProbes = 6  // bound the live Restart Manager probes
)

type diagSample struct {
	attempts int
	arm      string
	// errno seen on the attempt that crossed the production budget, if any.
	crossErrno syscall.Errno
}

type diagRecorder struct {
	mu      sync.Mutex
	samples []diagSample
	probes  []string
	nProbe  int
}

func (r *diagRecorder) add(s diagSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, s)
}

// probeLive asks who holds path open, RIGHT NOW, while the other writers of
// this arm are still running. Restart Manager is used (not an exclusive open)
// because a share-mode-0 CreateFile would itself block the very renames we are
// measuring and poison the experiment.
func (r *diagRecorder) probeLive(path string, attempt int, err error) {
	r.mu.Lock()
	if r.nProbe >= maxLiveProbes {
		r.mu.Unlock()
		return
	}
	r.nProbe++
	n := r.nProbe
	r.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "\n--- LIVE PROBE %d: %s denied at attempt %d (err=%v, errno=%d) ---\n",
		n, path, attempt, err, diagErrno(err))
	// Delete-pending signature: the NAME exists but any open of it is denied.
	if _, serr := os.Stat(path); serr != nil {
		fmt.Fprintf(&b, "  os.Stat(dest) FAILED: %v (errno=%d) <- delete-pending signature\n",
			serr, diagErrno(serr))
	} else {
		b.WriteString("  os.Stat(dest) ok (destination is present and openable)\n")
	}
	b.WriteString(diagRMHolders([]string{path, filepath.Dir(path)}))

	r.mu.Lock()
	r.probes = append(r.probes, b.String())
	r.mu.Unlock()
}

func diagErrno(err error) uintptr {
	var e syscall.Errno
	if ok := errorsAs(err, &e); ok {
		return uintptr(e)
	}
	return 0
}

// errorsAs avoids importing "errors" twice over; kept tiny and local.
func errorsAs(err error, target *syscall.Errno) bool {
	for err != nil {
		if e, ok := err.(syscall.Errno); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			// *os.LinkError / *os.PathError implement Unwrap; anything else stops here.
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// diagWrite mirrors atomicfile.WriteFile exactly, except that the rename
// retry budget is diagBudget and every attempt is counted. Production code is
// NOT modified by this file.
func diagWrite(rec *diagRecorder, arm, path string, b []byte, perm os.FileMode) (int, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return 0, err
	}
	tmp := f.Name()
	if _, err = f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return 0, err
	}
	if err = f.Close(); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err = os.Chmod(tmp, perm); err != nil {
		os.Remove(tmp)
		return 0, err
	}

	s := diagSample{arm: arm}
	attempts := 1
	err = os.Rename(tmp, path)
	for i := 0; err != nil && renameErrRecoverable(err) && i < diagBudget; i++ {
		if attempts == prodBudget {
			// This rename has just exhausted what production would allow.
			s.crossErrno = syscall.Errno(diagErrno(err))
			rec.probeLive(path, attempts, err)
		}
		time.Sleep(diagDelay)
		attempts++
		err = os.Rename(tmp, path)
	}
	s.attempts = attempts
	rec.add(s)
	if err != nil {
		os.Remove(tmp)
	}
	return attempts, err
}

func TestDiag6875_RenameContentionProfile(t *testing.T) {
	if os.Getenv("GRAFEL_DIAG_6875") == "" {
		t.Skip("throwaway #6875 instrumentation; set GRAFEL_DIAG_6875=1")
	}
	rec := &diagRecorder{}

	// ---- Arm A: the test's own shape. 8 concurrent writers, one destination.
	const writersA, iterationsA = 8, 40
	payloads := diagPayloads(writersA)
	dirA := t.TempDir()
	pathA := filepath.Join(dirA, "shared.json")
	for it := 0; it < iterationsA; it++ {
		var wg sync.WaitGroup
		errs := make([]error, writersA)
		start := make(chan struct{})
		for i := 0; i < writersA; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, errs[i] = diagWrite(rec, "A", pathA, payloads[i], 0o644)
			}(i)
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Errorf("arm A iteration %d writer %d: even a %d-attempt budget failed: %v",
					it, i, diagBudget, err)
			}
		}
	}

	// ---- Arm B: the control. ONE writer, same total write count, same
	// directory shape, same machine, same Defender/indexer exposure. Zero
	// self-contention by construction.
	dirB := t.TempDir()
	pathB := filepath.Join(dirB, "shared.json")
	for i := 0; i < writersA*iterationsA; i++ {
		if _, err := diagWrite(rec, "B", pathB, payloads[i%writersA], 0o644); err != nil {
			t.Errorf("arm B write %d: %v", i, err)
		}
	}

	report(t, rec)
}

func report(t *testing.T, rec *diagRecorder) {
	t.Helper()
	byArm := map[string][]int{}
	for _, s := range rec.samples {
		byArm[s.arm] = append(byArm[s.arm], s.attempts)
	}
	var out strings.Builder
	out.WriteString("\n================ #6875 RENAME CONTENTION PROFILE ================\n")
	for _, arm := range []string{"A", "B"} {
		a := byArm[arm]
		if len(a) == 0 {
			continue
		}
		sort.Ints(a)
		label := "8 concurrent writers -> one destination"
		if arm == "B" {
			label = "1 writer -> one destination (CONTROL)"
		}
		var retried, overProd int
		for _, v := range a {
			if v > 1 {
				retried++
			}
			if v > prodBudget {
				overProd++
			}
		}
		p := func(q float64) int { return a[int(float64(len(a)-1)*q)] }
		fmt.Fprintf(&out, "\nARM %s — %s\n", arm, label)
		fmt.Fprintf(&out, "  writes=%d  min=%d  p50=%d  p90=%d  p99=%d  max=%d\n",
			len(a), a[0], p(0.50), p(0.90), p(0.99), a[len(a)-1])
		fmt.Fprintf(&out, "  needed >1 attempt (i.e. hit ACCESS_DENIED at all): %d (%.2f%%)\n",
			retried, 100*float64(retried)/float64(len(a)))
		fmt.Fprintf(&out, "  would have EXCEEDED the production budget of %d: %d (%.2f%%)\n",
			prodBudget, overProd, 100*float64(overProd)/float64(len(a)))
	}
	out.WriteString("\n---------------- LIVE HOLDER PROBES ----------------\n")
	if len(rec.probes) == 0 {
		out.WriteString("  (no rename ever reached the production budget in this run)\n")
	}
	for _, p := range rec.probes {
		out.WriteString(p)
	}
	out.WriteString("\n================================================================\n")
	t.Log(out.String())
}

// ---- Restart Manager, copied from internal/testsupport/tempdirdiag_windows.go.
// Duplicated rather than exported: this file is throwaway.

const (
	diagRMSessionKeyLen = 33
	diagRMMaxProcInfo   = 32
)

type diagRMUniqueProcess struct {
	ProcessID        uint32
	ProcessStartTime windows.Filetime
}

type diagRMProcessInfo struct {
	Process          diagRMUniqueProcess
	AppName          [256]uint16
	ServiceShortName [64]uint16
	ApplicationType  uint32
	AppStatus        uint32
	TSSessionID      uint32
	Restartable      int32
}

var (
	diagModRstrtmgr             = windows.NewLazySystemDLL("rstrtmgr.dll")
	diagProcRmStartSession      = diagModRstrtmgr.NewProc("RmStartSession")
	diagProcRmRegisterResources = diagModRstrtmgr.NewProc("RmRegisterResources")
	diagProcRmGetList           = diagModRstrtmgr.NewProc("RmGetList")
	diagProcRmEndSession        = diagModRstrtmgr.NewProc("RmEndSession")
)

func diagRMHolders(paths []string) string {
	var b strings.Builder
	var session uint32
	key := make([]uint16, diagRMSessionKeyLen)
	r, _, _ := diagProcRmStartSession.Call(
		uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0])))
	if r != 0 {
		fmt.Fprintf(&b, "  RestartManager: RmStartSession failed (win32 error %d)\n", r)
		return b.String()
	}
	defer diagProcRmEndSession.Call(uintptr(session)) //nolint:errcheck // diagnostic teardown

	ptrs := make([]*uint16, 0, len(paths))
	for _, p := range paths {
		u, err := windows.UTF16PtrFromString(p)
		if err != nil {
			continue
		}
		ptrs = append(ptrs, u)
	}
	if len(ptrs) == 0 {
		return "  RestartManager: no registrable paths\n"
	}
	r, _, _ = diagProcRmRegisterResources.Call(
		uintptr(session), uintptr(len(ptrs)), uintptr(unsafe.Pointer(&ptrs[0])), 0, 0, 0, 0)
	if r != 0 {
		fmt.Fprintf(&b, "  RestartManager: RmRegisterResources failed (win32 error %d)\n", r)
		return b.String()
	}
	var needed uint32
	count := uint32(diagRMMaxProcInfo)
	infos := make([]diagRMProcessInfo, diagRMMaxProcInfo)
	var rebootReasons uint32
	r, _, _ = diagProcRmGetList.Call(
		uintptr(session),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&infos[0])),
		uintptr(unsafe.Pointer(&rebootReasons)))
	if r != 0 && r != uintptr(windows.ERROR_MORE_DATA) {
		fmt.Fprintf(&b, "  RestartManager: RmGetList failed (win32 error %d)\n", r)
		return b.String()
	}
	if needed == 0 {
		fmt.Fprintf(&b, "  RestartManager: NO process reported holding the destination.\n")
		fmt.Fprintf(&b, "    self pid=%d\n", os.Getpid())
		return b.String()
	}
	fmt.Fprintf(&b, "  RestartManager: %d holder(s) (self pid=%d)\n", needed, os.Getpid())
	for i := uint32(0); i < count && int(i) < len(infos); i++ {
		pi := infos[i]
		fmt.Fprintf(&b, "    pid=%d app=%q service=%q\n",
			pi.Process.ProcessID,
			windows.UTF16ToString(pi.AppName[:]),
			windows.UTF16ToString(pi.ServiceShortName[:]))
	}
	return b.String()
}

func diagPayloads(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = bytes.Repeat([]byte{byte('A' + i)}, 4096*(i+1)+i)
	}
	return out
}

package testsupport

// tempdirdiag.go — the #6512 Windows-only TempDir-cleanup diagnostic.
//
// # The failure this exists to explain
//
// On windows-latest, `internal/daemon` intermittently reddens with
//
//	testing.go:1464: TempDir RemoveAll cleanup: unlinkat
//	  C:\...\TestResolveMemLimitMB_DefaultClamped797493729\001:
//	  The directory is not empty.
//
// The test body passes; testing's own deferred RemoveAll is what fails, and
// Go reports a t.TempDir() cleanup error as a test failure — so a green test
// reddens the whole CI leg. On Windows an open handle blocks deletion, and
// ERROR_DIR_NOT_EMPTY after a SUCCESSFUL recursive child delete is the
// delete-pending signature: something still held a handle.
//
// # Why this is a diagnostic and not a fix
//
// #6512 records the measurements that already killed every in-repo suspect: a
// goroutine dump inside the failing test's own t.Cleanup showed exactly two
// live goroutines (the test func and testing.runTests); every GRAFEL_HOME
// resolver was instrumented and only the test's own stack appeared; the
// `orphanroot` sweep lines land strictly AFTER the FAIL and OrphanRootSweeper
// has no goroutine at all. Two attempts of an IDENTICAL commit failed
// DIFFERENT tests. Nine occurrences, all confined to the two t.TempDir()
// users in internal/daemon/memlimit_test.go, against 223 other
// t.Setenv(…, t.TempDir()) sites that have never failed.
//
// So the holder is genuinely unnamed, and a RemoveAll retry would convert an
// intermittent failure into a permanent false reassurance. The owner's
// decision on #6512 is the diagnostic arm only: capture enough, the next time
// it fires on CI, to NAME THE HOLDER.
//
// # The cleanup-ordering trick this is built on
//
// t.Cleanup runs LIFO, and testing registers its RemoveAll cleanup INSIDE the
// first t.TempDir() call. A cleanup registered AFTER t.TempDir() therefore
// runs BEFORE the RemoveAll — which is exactly why the earlier goroutine dump
// on #6512 could only observe the pre-cleanup state and saw nothing.
//
// Registering the diagnostic BEFORE calling t.TempDir() inverts that: it is
// the deepest cleanup on the stack, so it runs LAST, immediately AFTER the
// failed RemoveAll, while the handle is most likely still held.
//
// # What it must not do
//
// It must not change whether a test passes or fails. It only reads, logs, and
// (as an explicitly labelled probe) retries the removal of state RemoveAll has
// already abandoned. testing has already called t.Errorf by the time this
// runs, so nothing here can turn a red test green.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// residualScanLimit bounds the walk so a pathological residue cannot turn a
// diagnostic into a hang or a megabyte of CI log.
const residualScanLimit = 200

// ResidualEntry is one filesystem entry left behind after a RemoveAll that
// reported success on its children and then failed to remove the directory —
// or that failed outright. Path is absolute when the scanned root is.
type ResidualEntry struct {
	// Path is the full path of the entry.
	Path string
	// IsDir reports whether the entry is a directory. This is the half the
	// #6512 log cannot show: a base-level failure means the TempDir base
	// itself was delete-pending, while a `\001` failure means a CHILD of it
	// was, and only an enumeration distinguishes "001 had a real child" from
	// "001 was empty and still undeletable".
	IsDir bool
	// Size is the entry's size in bytes; meaningless for directories.
	Size int64
	// Mode is the entry's file mode.
	Mode fs.FileMode
	// ModTime is when the entry was last written. A residual file modified
	// after the test's own writes points at a holder outside the test.
	ModTime time.Time
	// StatErr is non-empty when the entry could be named by its parent's
	// directory listing but not stat'd. On Windows a delete-pending entry
	// stats as ERROR_ACCESS_DENIED, so this field is itself evidence.
	StatErr string
}

// EnumerateResidual walks root breadth-first and returns every entry still
// present, including root itself. A root that does not exist yields no entries
// and no error — that is the healthy case. A root that exists but cannot be
// stat'd (on Windows, a delete-pending directory stats as ERROR_ACCESS_DENIED)
// is returned as a single entry carrying StatErr, because that state is itself
// the finding. The returned error reports only a PARTIAL walk: some
// subdirectory could not be listed.
//
// It never follows symlinks: an entry is described by its own Lstat, so a
// dangling or looping link is reported rather than chased.
func EnumerateResidual(root string) ([]ResidualEntry, error) {
	fi, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		// The root exists in some form we cannot describe — on Windows a
		// delete-pending directory stats as access-denied. Report it as a
		// residual entry in its own right rather than losing the fact.
		return []ResidualEntry{{Path: root, IsDir: true, StatErr: err.Error()}}, nil
	}

	out := []ResidualEntry{entryFor(root, fi, "")}
	if !fi.IsDir() {
		return out, nil
	}

	// Breadth-first, so a shallow residue is fully described before the walk
	// runs into residualScanLimit on some deep subtree.
	queue := []string{root}
	var walkErr error
	for len(queue) > 0 && len(out) < residualScanLimit {
		dir := queue[0]
		queue = queue[1:]
		names, err := os.ReadDir(dir)
		if err != nil {
			if walkErr == nil {
				walkErr = err
			}
			continue
		}
		for _, de := range names {
			if len(out) >= residualScanLimit {
				break
			}
			p := filepath.Join(dir, de.Name())
			cfi, cerr := os.Lstat(p)
			if cerr != nil {
				out = append(out, ResidualEntry{Path: p, IsDir: de.IsDir(), StatErr: cerr.Error()})
				continue
			}
			out = append(out, entryFor(p, cfi, ""))
			if cfi.IsDir() {
				queue = append(queue, p)
			}
		}
	}
	return out, walkErr
}

func entryFor(path string, fi fs.FileInfo, statErr string) ResidualEntry {
	return ResidualEntry{
		Path:    path,
		IsDir:   fi.IsDir(),
		Size:    fi.Size(),
		Mode:    fi.Mode(),
		ModTime: fi.ModTime(),
		StatErr: statErr,
	}
}

// FormatResidual renders entries as a stable, greppable block for a CI log.
// Directories are tagged `dir ` and files `file`, because "was 001 empty?" is
// the first question #6512 needs answered and the raw failure text cannot say.
func FormatResidual(root string, entries []ResidualEntry, walkErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "residual entries under %s: %d\n", root, len(entries))
	if walkErr != nil {
		fmt.Fprintf(&b, "  (walk was partial: %v)\n", walkErr)
	}
	if len(entries) >= residualScanLimit {
		fmt.Fprintf(&b, "  (truncated at %d entries)\n", residualScanLimit)
	}
	for _, e := range entries {
		kind := "file"
		if e.IsDir {
			kind = "dir "
		}
		fmt.Fprintf(&b, "  [%s] %s", kind, e.Path)
		if e.StatErr != "" {
			fmt.Fprintf(&b, "  STAT-ERR=%v", e.StatErr)
		} else {
			fmt.Fprintf(&b, "  mode=%v size=%d mtime=%s", e.Mode, e.Size, e.ModTime.UTC().Format(time.RFC3339Nano))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// RemovalProbe re-attempts removal of each residual path, deepest first, and
// reports the outcome per path. It is a PROBE, not a retry policy: by the time
// it runs, testing has already recorded the cleanup failure via t.Errorf, so
// nothing it does can turn a red test green. Its value is that it separates
// two indistinguishable-from-the-log causes — a transient delete-pending state
// that clears within milliseconds, and a handle that is still genuinely held.
func RemovalProbe(entries []ResidualEntry) string {
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	// Longest path first. Within one tree a child's path is always longer
	// than its parent's, so this orders every parent after its children,
	// which is what makes a parent directory removable at all.
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })

	var b strings.Builder
	b.WriteString("second-removal probe (deepest first):\n")
	for _, p := range paths {
		err := os.Remove(p)
		switch {
		case err == nil:
			fmt.Fprintf(&b, "  REMOVED-ON-RETRY %s\n", p)
		case errors.Is(err, fs.ErrNotExist):
			fmt.Fprintf(&b, "  already-gone     %s\n", p)
		default:
			fmt.Fprintf(&b, "  STILL-HELD       %s: %v\n", p, err)
		}
	}
	return b.String()
}

// diagTB is the sliver of testing.TB the arming code uses. It exists so the
// cleanup-ORDERING property — the entire mechanism of this file — can be
// observed by a test on any platform, instead of being asserted only in prose
// and left to a Windows CI leg nobody can run on demand.
type diagTB interface {
	Helper()
	Name() string
	Cleanup(func())
	Logf(string, ...any)
	TempDir() string
}

// diagOut is where the diagnostic is mirrored in addition to the test log.
// A variable so a test can capture it; it is os.Stderr everywhere else.
var diagOut io.Writer = os.Stderr

// TempDirWithCleanupDiagnostic returns t.TempDir(), and on Windows ONLY also
// arms the #6512 diagnostic: if testing's deferred RemoveAll leaves anything
// behind, the residual entries, a per-path removal probe and the identity of
// every process holding one of them are written to the test log.
//
// On every other GOOS it is exactly t.TempDir() — no extra cleanup is
// registered, so macOS and Linux legs carry nothing at all.
//
// Calling it twice in one test arms the diagnostic twice against the same
// TempDir base; the second report is a duplicate, not a fault. Prefer one call
// per test, which is what every #6512 call site does.
func TempDirWithCleanupDiagnostic(t testing.TB) string {
	t.Helper()
	return armTempDirDiagnostic(t, runtime.GOOS)
}

// armTempDirDiagnostic is TempDirWithCleanupDiagnostic with the platform
// decision passed in rather than read from runtime, so both branches are
// reachable from a test on one machine.
func armTempDirDiagnostic(t diagTB, goos string) string {
	t.Helper()
	if goos != "windows" {
		return t.TempDir()
	}
	// Registered BEFORE t.TempDir() so that LIFO ordering puts it AFTER
	// testing's RemoveAll cleanup. See the file comment — this ordering is
	// the whole mechanism, and reversing these two statements silently turns
	// the diagnostic into the same pre-cleanup snapshot that already told
	// #6512 nothing.
	var base string
	t.Cleanup(func() { reportTempDirResidual(t, base) })

	dir := t.TempDir()
	// testing hands out <base>/001, <base>/002, ... and RemoveAll's target is
	// <base>. A base-level failure and a child-level failure are different
	// #6512 shapes, so scan from the base.
	base = filepath.Dir(dir)
	return dir
}

// reportTempDirResidual is the armed cleanup. It stays silent unless the base
// survived RemoveAll, so a healthy run adds nothing to the log.
func reportTempDirResidual(t diagTB, base string) {
	t.Helper()
	if base == "" {
		return
	}
	// EnumerateResidual reports nothing for a base that no longer exists, so
	// this single guard is what keeps a healthy run silent. An earlier draft
	// also short-circuited on an ErrNotExist Lstat; that guard was removed
	// because deleting it changed no observable behaviour and no test could
	// go red for it, which makes it decoration rather than a property.
	entries, walkErr := EnumerateResidual(base)
	if len(entries) == 0 {
		return // cleanup succeeded; nothing to say.
	}

	var b strings.Builder
	b.WriteString("\n=== #6512 WINDOWS TEMPDIR CLEANUP DIAGNOSTIC ===\n")
	fmt.Fprintf(&b, "test: %s\n", t.Name())
	b.WriteString(FormatResidual(base, entries, walkErr))

	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	b.WriteString(describeHolders(paths))
	b.WriteString(RemovalProbe(entries))
	b.WriteString("=== end #6512 diagnostic ===\n")

	// t.Logf attaches the block to the already-failed test, which is where a
	// reader looking at a red Windows leg will actually find it - no special
	// flag, no re-run. It is also mirrored to stderr because `go test -json`
	// consumers have been known to drop cleanup-phase output.
	t.Logf("%s", b.String())
	fmt.Fprint(diagOut, b.String())
}

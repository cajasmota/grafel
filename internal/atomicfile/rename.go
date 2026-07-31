package atomicfile

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

// rename.go — the "replace the destination" half of an atomic write.
//
// # Why os.Rename alone is not enough on Windows (#6053)
//
// On POSIX, rename(2) replaces the destination unconditionally: the file's own
// mode is irrelevant (write permission on the DIRECTORY is what governs) and
// other processes holding the destination open do not block it — they keep
// reading the old inode. So on unix this file is inert by construction: the
// platform predicates in rename_other.go are constant false/no-op and
// renameOver degenerates to exactly one os.Rename.
//
// Windows has neither property, and two distinct failures follow. Both were
// caught by the existing test suite the first time it ran on windows-latest:
//
//  1. READ-ONLY DESTINATION — deterministic. Go's os.Rename on Windows already
//     IS MoveFileEx(from, to, MOVEFILE_REPLACE_EXISTING) (syscall.Rename), so
//     "call MoveFileEx by hand via golang.org/x/sys/windows" buys nothing;
//     that was the first thing checked. REPLACE_EXISTING refuses to replace a
//     file carrying FILE_ATTRIBUTE_READONLY and returns ERROR_ACCESS_DENIED,
//     forever. Retrying cannot fix it. The only remedy is to clear the
//     attribute — see the warning contract on warnReadOnlyCleared below.
//
//  2. SHARING VIOLATION — transient. Replacing a destination needs a handle
//     with DELETE access, which Windows refuses while anyone holds the file
//     open without FILE_SHARE_DELETE. Go's own os.OpenFile always passes
//     FILE_SHARE_DELETE, so grafel is never the offender; the openers we
//     cannot control are the concurrent replacer's own in-flight MoveFileEx,
//     a concurrent reader mid-os.ReadFile, Defender's real-time scan of the
//     just-created temp file, and the search indexer. Nothing in this process
//     can prevent those and they clear in microseconds, so a bounded retry is
//     the only remedy available.
//
// A retry alone would be wrong: it would spend the whole budget failing on
// case 1 and then report the same error. Clearing read-only alone would be
// wrong: it does nothing for case 2. Both are needed, and the order matters —
// the deterministic case is handled first, without sleeping.
//
// # This is the SIXTH copy of the Windows retry loop in this tree
//
// Stated plainly because an earlier draft of this comment claimed it was the
// second. The existing five are internal/graph/groupalgo,
// internal/graph/descriptions and internal/graph/flows (three copies of
// atomicRename), internal/statusfile and internal/install. This one is
// deliberately aligned with them — same ~40 × 5ms budget, same
// fs.ErrPermission-first classification, same golang.org/x/sys/windows
// constants, same vars-not-consts so a test can shrink the delay — rather than
// inventing a sixth dialect. Consolidating all six behind this package is the
// right end state and is explicitly NOT done here: this branch is
// release-blocking and that sweep is not.
//
// Two consequences follow from the case-1 analysis above, and are expected
// rather than surprising:
//
//   - graph/{groupalgo,descriptions,flows}.atomicRename are STILL broken for a
//     read-only destination, and will now burn the full ~200ms per write
//     retrying something retrying cannot fix.
//   - ~50 production sites still call bare os.Rename for temp→dest with no
//     retry at all (graph/manifest.go, graph/graph.go, graph/fbwriter/*,
//     mcp/repair.go, enrichment/candidates.go, dashboard/*,
//     daemon/requests/requests.go).
//
// If windows-latest goes red again on one of those, that is the next slice of
// the same defect class, not a regression from this change.

// renameRetries / renameRetryDelay bound case 2.
//
// The budget is sized on ATTEMPT COUNT, not wall-clock. The tests this must
// fix are 8-way concurrent (links: 8 writers × 20 iterations = 160 writes;
// atomicfile: 8 × 40) with Defender and the indexer competing on top, so a
// writer that loses N consecutive races fails — and N, not elapsed time, is
// what has to exceed the contention. 40 × 5ms ≈ 200ms matches the in-tree
// precedent (internal/graph/groupalgo/atomicrename_windows.go), whose own
// comment reasons against a FOUR-reader stress test; 8 concurrent writers is
// strictly heavier, so anything below that precedent would be a regression in
// disguise. An earlier draft of this file used 8 attempts with exponential
// backoff, which optimised the wall-clock number while making the attempt
// count worse — exactly the wrong axis.
//
// Bounded on purpose: if a handle genuinely holds the destination for >200ms,
// the write surfaces the real error rather than hanging a link pass. Vars, not
// consts, so a future test can shrink the delay (as all five precedents do).
var (
	renameRetries    = 40
	renameRetryDelay = 5 * time.Millisecond
)

// renameOps is the set of primitives renameOver drives. Splitting them out is
// what makes the Windows recovery logic testable on a machine that is not
// Windows: rename_test.go substitutes fakes, and the real implementations in
// rename_windows.go / rename_other.go stay small enough to review by eye.
type renameOps struct {
	rename      func(oldpath, newpath string) error
	isReadOnly  func(path string) bool
	setReadOnly func(path string, ro bool) error
	recoverable func(err error) bool
	sleep       func(d time.Duration)
	warn        func(path string)
}

var defaultRenameOps = renameOps{
	rename:      os.Rename,
	isReadOnly:  destIsReadOnly,
	setReadOnly: setDestReadOnly,
	recoverable: renameErrRecoverable,
	sleep:       time.Sleep,
	warn:        warnReadOnlyCleared,
}

// warnReadOnlyCleared reports that a user's read-only protection was removed.
//
// On Windows FILE_ATTRIBUTE_READONLY is the user-facing "protect this file"
// checkbox, and no production call site here ever asks for a read-only mode —
// every atomicfile.WriteFile in the tree passes 0644 or 0600. So the ONLY way
// this branch runs in production is that a HUMAN marked the destination
// read-only, and clearing it is permanent: the replacement file carries the
// caller's perm, which is writable. Before #6053 grafel reported "Access is
// denied" and left the file alone; now it wins, silently, unless this says so.
//
// The pre-existing contract (TestWriteFile_OverwritesReadOnlyDestination, and
// the package doc's "a READ-ONLY destination is REPLACED") is deliberate and
// unix has always behaved this way — but on unix the DIRECTORY's permission
// still gates it, and on Windows nothing does. Hence a warning rather than an
// error: the contract stands, the silence does not.
func warnReadOnlyCleared(path string) {
	slog.Warn("atomicfile: cleared the read-only attribute on a destination in order to replace it; "+
		"the file's read-only protection is now gone",
		"path", path)
}

// renameAtomic renames tmp over path, recovering from the two Windows-only
// failures described above. On unix it is one os.Rename and nothing else.
func renameAtomic(tmp, path string) error {
	return defaultRenameOps.renameOver(tmp, path)
}

func (o renameOps) renameOver(tmp, path string) (err error) {
	attempts := 1
	if err = o.rename(tmp, path); err == nil || !o.recoverable(err) {
		return err
	}

	// Case 1: a read-only destination. Deterministic — handled before any
	// sleep, because no amount of waiting changes a file attribute.
	if o.isReadOnly(path) && o.setReadOnly(path, false) == nil {
		o.warn(path)
		// If we end up NOT replacing the destination, put its protection
		// back: a failed write must not leave the file writable.
		defer func() {
			if err != nil {
				_ = o.setReadOnly(path, true)
			}
		}()
		attempts++
		if err = o.rename(tmp, path); err == nil {
			return nil
		}
		if !o.recoverable(err) {
			return err
		}
	}

	// Case 2: someone else holds a handle without FILE_SHARE_DELETE.
	for i := 0; i < renameRetries; i++ {
		o.sleep(renameRetryDelay)
		attempts++
		if err = o.rename(tmp, path); err == nil {
			return nil
		}
		if !o.recoverable(err) {
			return err
		}
	}
	// Wrapped with the attempt count so an exhausted budget is
	// distinguishable in CI output from no retry having happened at all —
	// otherwise a badly sized budget and a missing retry loop produce
	// identical failures.
	return fmt.Errorf("rename %s -> %s: still failing after %d attempts over %v: %w",
		tmp, path, attempts, time.Duration(renameRetries)*renameRetryDelay, err)
}

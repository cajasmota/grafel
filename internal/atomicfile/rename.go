package atomicfile

import (
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
//     "call MoveFileEx via golang.org/x/sys/windows instead" buys nothing;
//     that was the first thing checked. REPLACE_EXISTING refuses to replace a
//     file carrying FILE_ATTRIBUTE_READONLY and returns ERROR_ACCESS_DENIED,
//     forever. Retrying cannot fix it. The only remedy is to clear the
//     attribute, which is what setDestReadOnly does — and, because that
//     mutates a file we were asked to replace but might still fail to replace,
//     it is put back on the failure path.
//
//  2. SHARING VIOLATION — transient. Replacing a destination needs a handle
//     with DELETE access, which Windows refuses while anyone holds the file
//     open without FILE_SHARE_DELETE. Go's own os.OpenFile always passes
//     FILE_SHARE_DELETE, so grafel is never the offender; the openers we
//     cannot control are the concurrent replacer's own in-flight MoveFileEx,
//     Defender's real-time scan of the just-created temp file, and the search
//     indexer. Nothing in this process can prevent those, and they clear in
//     milliseconds — a bounded retry is the only remedy available, and it is
//     the same remedy Git for Windows, Chromium and Rust's std adopted.
//
// A retry alone would be wrong: it would spend half a second failing on case 1
// and then report the same error. Clearing read-only alone would be wrong: it
// does nothing for case 2. Both are needed, and the order matters — the
// deterministic case is handled first, without sleeping.

// renameRetries bounds case 2. The backoff doubles from renameRetryBase, so
// eight retries wait ~510ms in total; a write blocks for at most that before
// surfacing the error. Long enough for an antivirus scan of a small state
// file, short enough that a wedged handle is reported rather than hidden.
const (
	renameRetries   = 8
	renameRetryBase = 2 * time.Millisecond
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
}

var defaultRenameOps = renameOps{
	rename:      os.Rename,
	isReadOnly:  destIsReadOnly,
	setReadOnly: setDestReadOnly,
	recoverable: renameErrRecoverable,
	sleep:       time.Sleep,
}

// renameAtomic renames tmp over path, recovering from the two Windows-only
// failures described above. On unix it is one os.Rename and nothing else.
func renameAtomic(tmp, path string) error {
	return defaultRenameOps.renameOver(tmp, path)
}

func (o renameOps) renameOver(tmp, path string) (err error) {
	if err = o.rename(tmp, path); err == nil || !o.recoverable(err) {
		return err
	}

	// Case 1: a read-only destination. Deterministic — handled before any
	// sleep, because no amount of waiting changes a file attribute.
	if o.isReadOnly(path) && o.setReadOnly(path, false) == nil {
		// If we end up NOT replacing the destination, put its protection
		// back: a failed write must not leave the file writable.
		defer func() {
			if err != nil {
				_ = o.setReadOnly(path, true)
			}
		}()
		if err = o.rename(tmp, path); err == nil {
			return nil
		}
		if !o.recoverable(err) {
			return err
		}
	}

	// Case 2: someone else holds a handle without FILE_SHARE_DELETE.
	delay := renameRetryBase
	for i := 0; i < renameRetries; i++ {
		o.sleep(delay)
		delay *= 2
		if err = o.rename(tmp, path); err == nil {
			return nil
		}
		if !o.recoverable(err) {
			return err
		}
	}
	return err
}

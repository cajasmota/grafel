package walk

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/cajasmota/grafel/internal/safeio"
)

// maxIgnoreFileBytes caps a single ignore-file read.
//
// The cap is not belt-and-braces. safeio's type gate already makes a FIFO
// impossible, but a character device is the second shape of #6416: /dev/zero
// opens fine and never reaches EOF, so "it will hit EOF eventually" is not a
// bound. 8 MiB is several thousand times the largest plausible .grafelignore;
// a file past it is truncated, which degrades to the same "patterns not fully
// applied" outcome an unreadable file already produces.
const maxIgnoreFileBytes = 8 << 20

// ignoreSkip* back the always-on report below.
var (
	ignoreSkipMu   sync.Mutex
	ignoreSkipSeen map[string]bool
	ignoreSkipOut  io.Writer = os.Stderr
)

// maxIgnoreSkipReports caps the report the way IrregularSkipReport,
// reportLicenseSkip and reportGoModSkip cap theirs: a warning long enough to
// scroll past reports nothing. The population here is bounded by the depth of
// the indexed path below its git root, so the cap is a backstop rather than
// load-bearing — but a pathological monorepo path is exactly the tree where a
// per-ancestor line would be worst.
const maxIgnoreSkipReports = 8

// setIgnoreSkipOutput redirects the report for tests and returns a restore
// func. Test-only helper.
func setIgnoreSkipOutput(w io.Writer) func() {
	ignoreSkipMu.Lock()
	prev := ignoreSkipOut
	ignoreSkipOut = w
	ignoreSkipSeen = nil
	ignoreSkipMu.Unlock()
	return func() {
		ignoreSkipMu.Lock()
		ignoreSkipOut = prev
		ignoreSkipSeen = nil
		ignoreSkipMu.Unlock()
	}
}

// readIgnoreFile is the only way this package reads an ignore file by NAME.
//
// The distinction from ParseIgnoreFile matters. That reader goes through
// openWithDeadline, which lstats and refuses a non-regular entry before it
// opens anything — it has been gated since #1729. The inherited-.grafelignore
// path had no such gate: it joined a literal filename onto an ancestor
// directory and handed the result straight to os.ReadFile. That it sat inside
// the very package whose entry-type gate #6468 added is the point — the gate
// applies to entries the WALKER produced, and this read runs before the walk
// starts.
//
// The error is returned unchanged so callers keep their existing "treat any
// read failure as no patterns" behaviour; the skip is reported here so that
// behaviour stops being silent.
func readIgnoreFile(path string) ([]byte, error) {
	b, err := safeio.ReadFile(path, safeio.FollowSymlinks, maxIgnoreFileBytes)
	if err != nil {
		reportIgnoreSkip(path, err)
	}
	return b, err
}

// reportIgnoreSkip says out loud that an ignore file was refused for being a
// FIFO, device or socket.
//
// The consequence of a silent skip here is not "one file missing": a
// .grafelignore that stops applying puts every path it excluded BACK into the
// index, so the observable symptom is an index that grew for no stated reason.
// #6338 is the standing evidence that an omission of that shape costs far more
// to diagnose later than a line costs to print now.
//
// Only ErrNotRegular / ErrWouldBlock are reported. A plain ENOENT is the
// ORDINARY case — every ancestor directory between the git root and the
// indexed path is probed for a .grafelignore and nearly all of them lack one —
// so announcing it would emit several lines for every healthy repo and bury
// the signal this report exists to carry.
func reportIgnoreSkip(path string, err error) {
	if !errors.Is(err, safeio.ErrNotRegular) && !errors.Is(err, safeio.ErrWouldBlock) {
		return
	}
	ignoreSkipMu.Lock()
	if ignoreSkipSeen == nil {
		ignoreSkipSeen = map[string]bool{}
	}
	if ignoreSkipSeen[path] || len(ignoreSkipSeen) >= maxIgnoreSkipReports {
		ignoreSkipMu.Unlock()
		return
	}
	ignoreSkipSeen[path] = true
	last := len(ignoreSkipSeen) == maxIgnoreSkipReports
	w := ignoreSkipOut
	ignoreSkipMu.Unlock()

	fmt.Fprintf(w, "grafel: skipped %v — not read because reading one can block forever; the ignore rules it declares will not be applied (#6416)\n", withSkipPath(path, err))
	if last {
		fmt.Fprintf(w, "grafel: further ignore-file skips suppressed after %d\n", maxIgnoreSkipReports)
	}
}

// withSkipPath makes a skip line attributable.
//
// safeio's two reportable errors are not shaped alike: ErrNotRegular is
// wrapped with the path and the entry kind, but ErrWouldBlock is returned BARE
// from openWithDeadline's deadline arms. Printing it unadorned gives "skipped
// safeio: open would block", which names no file and so tells a user nothing
// they can act on — the same silence the report exists to end. Only the bare
// form is decorated, so ErrNotRegular's own wording is not printed twice.
func withSkipPath(path string, err error) error {
	if errors.Is(err, safeio.ErrWouldBlock) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return err
}

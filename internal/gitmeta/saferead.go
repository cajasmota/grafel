package gitmeta

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/cajasmota/grafel/internal/safeio"
)

// Byte caps for the two shapes of file this package reads.
//
// The caps are not belt-and-braces. safeio's type gate already makes a FIFO
// impossible, but a character device is the second shape of #6416: /dev/zero
// opens fine and never reaches EOF, so "it will hit EOF eventually" is not a
// bound.
const (
	// maxGitPointerBytes covers HEAD, commondir, the .git gitdir-file and a
	// loose ref. Every one of them is a single short line — a loose ref is
	// exactly 41 bytes — so 64 KiB is four orders of magnitude of headroom and
	// anything past it is not a git pointer file.
	maxGitPointerBytes = 64 << 10

	// maxSparsePatternBytes covers info/sparse-checkout, which is a pattern
	// list and legitimately large in a big monorepo. 8 MiB truncates only a
	// file that is already pathological.
	//
	// A truncated pattern file is NOT the same outcome as an unreadable one and
	// the two must not be conflated (#6969): unreadable lands on "not sparse"
	// and indexes the repo in FULL, while a silently-truncated read used to
	// leave the repo sparse with a partial pattern set, so files were dropped
	// from the index with no error, no SkipEntry and no missing-entity signal.
	// parseSparsePatternFile now reads the truncation signal and lands
	// truncated on "not sparse" too.
	maxSparsePatternBytes = 8 << 20
)

// gitMetaSkip* back the always-on report below.
var (
	gitMetaSkipMu   sync.Mutex
	gitMetaSkipSeen map[string]bool
	gitMetaSkipOut  io.Writer = os.Stderr
)

// maxGitMetaSkipReports caps the report the way walk.IrregularSkipReport,
// reportLicenseSkip and reportGoModSkip cap theirs: a warning long enough to
// scroll past reports nothing. The population is bounded — a handful of files
// per repo — so this is a backstop, but the daemon calls into this package on
// every whoami/ResolveCWD, so an uncapped per-call line would repeat forever.
const maxGitMetaSkipReports = 8

// setGitMetaSkipOutput redirects the report for tests and returns a restore
// func. Test-only helper.
func setGitMetaSkipOutput(w io.Writer) func() {
	gitMetaSkipMu.Lock()
	prev := gitMetaSkipOut
	gitMetaSkipOut = w
	gitMetaSkipSeen = nil
	gitMetaSkipMu.Unlock()
	return func() {
		gitMetaSkipMu.Lock()
		gitMetaSkipOut = prev
		gitMetaSkipSeen = nil
		gitMetaSkipMu.Unlock()
	}
}

// readGitMetaFile is the only way this package reads a file off disk.
//
// Consolidating the five call sites into one function is the point: every
// round of #6468 fixed the sites a reviewer had named and swept no further. A
// single reader means the next git file this package learns to parse is
// guarded by construction rather than by whoever remembers.
//
// WHY THIS PACKAGE IS THE WORST PLACE TO LEAVE THE HANG. Everything here runs
// on EVERY repo BEFORE any walk begins — headPointerKey and commitToken are on
// the CaptureCached / CaptureCachedFresh path that the daemon, ResolveCWD and
// `grafel index` all enter first. A block here is not a file missing from an
// index; it is an index that never starts, and no gate downstream can help
// because nothing downstream has run yet.
//
// The error is returned unchanged so callers keep their existing "treat any
// read failure as unknown" behaviour; the skip is reported here so that
// behaviour stops being silent.
func readGitMetaFile(path string, maxBytes int64) ([]byte, error) {
	b, _, err := readGitMetaFileLimited(path, maxBytes)
	return b, err
}

// readGitMetaFileLimited is readGitMetaFile plus the truncation signal.
//
// The two exist side by side on purpose. For this package's POINTER files —
// HEAD, commondir, the .git gitdir-file, a loose ref — a truncated read is
// already loud: each is one short line, every parser here demands a shape
// ("ref: ", "gitdir: ", 40 hex bytes), and a 64 KiB prefix of something else
// fails that shape and falls through to the same "unknown" the callers already
// handle. Those callers keep readGitMetaFile and are deliberately unchanged.
//
// The sparse pattern file is the one LIST-shaped reader here, and a prefix of
// a list is a valid-looking shorter list. That is the whole of #6969: it fails
// silently and it fails toward EXCLUSION, the one direction nothing downstream
// can notice.
func readGitMetaFileLimited(path string, maxBytes int64) ([]byte, bool, error) {
	b, truncated, err := safeio.ReadFileLimited(path, safeio.FollowSymlinks, maxBytes)
	if err != nil {
		reportGitMetaSkip(path, err)
	}
	return b, truncated, err
}

// reportGitMetaTruncation announces a git metadata file that was read only in
// part because it exceeded its byte cap.
//
// It is a SEPARATE report from reportGitMetaSkip and not a new branch inside
// it, because the two carry different facts: a skip says the file was not read
// at all and names the entry kind; this says the file was read and the tail is
// missing. It shares the dedup map and the cap, so the combined output is
// still bounded the way a report nobody scrolls past has to be.
func reportGitMetaTruncation(path string, maxBytes int64) {
	gitMetaSkipMu.Lock()
	if gitMetaSkipSeen == nil {
		gitMetaSkipSeen = map[string]bool{}
	}
	if gitMetaSkipSeen[path] || len(gitMetaSkipSeen) >= maxGitMetaSkipReports {
		gitMetaSkipMu.Unlock()
		return
	}
	gitMetaSkipSeen[path] = true
	last := len(gitMetaSkipSeen) == maxGitMetaSkipReports
	w := gitMetaSkipOut
	gitMetaSkipMu.Unlock()

	fmt.Fprintf(w, "grafel: %s exceeds %d bytes and was read only in part; treating this repo as NOT sparse so no file is silently excluded (#6969)\n", path, maxBytes)
	if last {
		fmt.Fprintf(w, "grafel: further git-metadata skips suppressed after %d\n", maxGitMetaSkipReports)
	}
}

// reportGitMetaSkip says out loud that a git metadata file was refused for
// being a FIFO, device or socket.
//
// The consequence of a silent skip differs per file and is never nothing: a
// refused HEAD or commondir makes commitToken unresolvable, which turns every
// CaptureCachedFresh call into an uncached live Capture (correct, but the
// memo #6079 exists for is gone); a refused sparse-checkout file drops the
// pattern set, so a sparse repo is indexed as if it were full. Both are
// #6338's shape — a behaviour change with no stated cause — unless the skip
// is announced.
//
// Only ErrNotRegular / ErrWouldBlock are reported. A plain ENOENT is the
// ORDINARY case throughout this package — commondir exists only in a linked
// worktree, packed-refs only after a gc, sparse-checkout only in a sparse
// clone — so announcing it would emit lines for every healthy repo and bury
// the signal this report exists to carry.
func reportGitMetaSkip(path string, err error) {
	if !errors.Is(err, safeio.ErrNotRegular) && !errors.Is(err, safeio.ErrWouldBlock) {
		return
	}
	gitMetaSkipMu.Lock()
	if gitMetaSkipSeen == nil {
		gitMetaSkipSeen = map[string]bool{}
	}
	if gitMetaSkipSeen[path] || len(gitMetaSkipSeen) >= maxGitMetaSkipReports {
		gitMetaSkipMu.Unlock()
		return
	}
	gitMetaSkipSeen[path] = true
	last := len(gitMetaSkipSeen) == maxGitMetaSkipReports
	w := gitMetaSkipOut
	gitMetaSkipMu.Unlock()

	fmt.Fprintf(w, "grafel: skipped %v — not read because reading one can block forever; this repo's git metadata will be incomplete (#6416)\n", withGitMetaPath(path, err))
	if last {
		fmt.Fprintf(w, "grafel: further git-metadata skips suppressed after %d\n", maxGitMetaSkipReports)
	}
}

// withGitMetaPath makes a skip line attributable.
//
// safeio's two reportable errors are not shaped alike: ErrNotRegular is
// wrapped with the path and the entry kind, but ErrWouldBlock is returned BARE
// from openWithDeadline's deadline arms. Printing it unadorned gives "skipped
// safeio: open would block", which names no file and so tells a user nothing
// they can act on — the same silence the report exists to end. Only the bare
// form is decorated, so ErrNotRegular's own wording is not printed twice.
func withGitMetaPath(path string, err error) error {
	if errors.Is(err, safeio.ErrWouldBlock) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return err
}

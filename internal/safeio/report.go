package safeio

// report.go — the shared "say out loud that a read was refused" layer (#6478).
//
// # Why this is in safeio and not copy-pasted a fifteenth time
//
// docs/blocking-open-audit.md states the rule that made rounds 2 and 3 of
// #6416 insufficient: a site only leaves the name-chosen-open bucket when it is
// BOTH routed through safeio AND reports what it skipped. Those two rounds
// routed internal/install/detect and internal/secrets and then mapped
// ErrNotRegular / ErrWouldBlock to a bare `return nil` — the hang was closed
// and the silence was kept, so `mkfifo creds.go` produced a secrets scan that
// answered "clean" without ever reading creds.go. That is #6338's shape.
//
// #6478 routes 27 further call sites across fourteen packages. Hand-rolling
// the dedup/cap/gate dance in fourteen more places is how the ORIGINAL defect
// happened: safeio's own doc comment says it exists "rather than a fourth
// hand-rolled copy of the same dance", and a fifteenth copy of the REPORTING
// half would be the same mistake one layer out. internal/licenses and
// internal/secrets keep their bespoke reporters — rewriting working code was
// not in scope — but nothing new should grow one.
//
// # The convention, unchanged from the packages that already implement it
//
//   - Always-on, to stderr. Not behind a verbosity flag: the whole point is
//     that a file vanished and nobody could tell why.
//   - Deduplicated by path, so a re-read does not repeat.
//   - Capped, with an explicit suppression notice, so a hostile tree cannot
//     turn the report into the denial-of-service it is warning about.
//   - Gated to ErrNotRegular / ErrWouldBlock ONLY. ENOENT is the ordinary case
//     at nearly every one of these sites — they probe for .gitignore, CLAUDE.md,
//     .grafel/group.json and expect absence — so reporting it would emit
//     several lines per healthy repo and train everyone to ignore the channel.

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// maxSkipReports caps the reports emitted per process, matching the cap
// internal/licenses and internal/daemon/walk already use.
const maxSkipReports = 16

// MaxConfigFileBytes bounds a read of a configuration, rules or agent file —
// .gitignore, CLAUDE.md, .grafel/group.json, a git hook, a coverage report.
//
// It is one shared number rather than fourteen hand-picked ones because the
// bound's PURPOSE is not per-file tuning: a character device never reaches EOF,
// so ReadFile without a cap reads until memory does, and any finite number
// closes that. 8 MiB is far above every real instance of these files and far
// below a machine's memory, which is the whole requirement.
const MaxConfigFileBytes = 8 << 20

var (
	skipMu   sync.Mutex
	skipSeen map[string]bool
	skipHush bool
)

// ReportSkip announces that path was refused for being a non-regular file, or
// because the open would have blocked. Any other error — including the
// overwhelmingly common ENOENT — is ignored.
func ReportSkip(path string, err error) {
	if !errors.Is(err, ErrNotRegular) && !errors.Is(err, ErrWouldBlock) {
		return
	}
	skipMu.Lock()
	defer skipMu.Unlock()
	if skipSeen == nil {
		skipSeen = map[string]bool{}
	}
	if skipSeen[path] {
		return
	}
	if len(skipSeen) >= maxSkipReports {
		if !skipHush {
			skipHush = true
			fmt.Fprintf(os.Stderr, "grafel: further non-regular-file skips suppressed after %d\n", maxSkipReports)
		}
		return
	}
	skipSeen[path] = true
	// ErrWouldBlock is returned BARE by Open — it carries no path — so the
	// path is prepended here rather than relying on the error to name it. A
	// warning that names no file is not a safety net.
	fmt.Fprintf(os.Stderr, "grafel: skipped %s: %v\n", path, err)
}

// ReadFileReported is ReadFile plus ReportSkip. It is the one-line replacement
// for os.ReadFile at a name-chosen site: the error is returned UNCHANGED, so a
// caller's existing "any read failure means absent" behaviour is preserved
// exactly, and the skip stops being invisible.
func ReadFileReported(path string, policy SymlinkPolicy, maxBytes int64) ([]byte, error) {
	b, err := ReadFile(path, policy, maxBytes)
	if err != nil {
		ReportSkip(path, err)
	}
	return b, err
}

// OpenReported is Open plus ReportSkip. The caller closes the returned file.
func OpenReported(path string, policy SymlinkPolicy) (*os.File, error) {
	f, err := Open(path, policy)
	if err != nil {
		ReportSkip(path, err)
	}
	return f, err
}

// resetSkipReportsForTest lets the package's own tests exercise the cap
// without leaking dedup state between cases.
func resetSkipReportsForTest() {
	skipMu.Lock()
	defer skipMu.Unlock()
	skipSeen = nil
	skipHush = false
}

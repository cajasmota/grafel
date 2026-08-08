package watch

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Descriptor cost model (#6180)
//
// The watcher's only ceiling used to be walk.DefaultWatchDirCap, which counts
// DIRECTORIES. On macOS that number is not the cost. fsnotify v1.10.1 ships no
// FSEvents backend — backend_kqueue.go carries `//go:build ... || darwin` — and
// kqueue cannot watch a path without an open descriptor on it. Worse, for a
// directory fsnotify additionally calls watchDirectoryFiles(), opening one more
// descriptor for EVERY FILE in that directory (fsnotify documents this in its
// own Close() comment).
//
// So a single subscription costs dirs + files-in-those-dirs descriptors, and a
// directory-only cap is blind to the term that dominates: a live measurement on
// one repo showed 40,079 descriptors, 32,073 of them regular files and 7,995
// directories — the per-file term was 4x the per-directory term. Against
// kern.maxfilesperproc = 61,440 that is 65% of the process ceiling for ONE
// repo, so at 100,000 directories the old cap was effectively infinite.
//
// On Linux, inotify spends a watch descriptor per directory out of
// max_user_watches, not a process file descriptor per file; on Windows the
// ReadDirectoryChangesW backend differs again. The per-file term is a macOS
// fact, so the cost model — and whether the budget is enabled at all — is
// selected by build tag, never by a runtime GOOS check (#6218).
// ---------------------------------------------------------------------------

// fdCostModel says how many descriptors the platform's fsnotify backend spends
// on a subscription of a given shape.
type fdCostModel struct {
	perDir  int
	perFile int
}

// cost returns the descriptor cost of watching dirs directories that between
// them contain files files.
func (m fdCostModel) cost(dirs, files int) int {
	return dirs*m.perDir + files*m.perFile
}

// kqueueCostModel is the macOS/BSD reality: one descriptor for the directory
// plus one for each file inside it.
var kqueueCostModel = fdCostModel{perDir: 1, perFile: 1}

// inotifyCostModel is the Linux reality: a watch per directory, drawn from
// max_user_watches rather than from RLIMIT_NOFILE, and nothing per file.
var inotifyCostModel = fdCostModel{perDir: 1, perFile: 0}

// ---------------------------------------------------------------------------
// The budget
// ---------------------------------------------------------------------------

// errFDBudget is returned by AddRepo when a subscription cannot be afforded
// within the process-wide descriptor budget. Callers get a real error instead
// of a repo that is registered but silently receives no events.
var errFDBudget = errors.New("watch: file-descriptor budget exhausted")

// isFDBudgetError reports whether err is (or wraps) the budget refusal.
func isFDBudgetError(err error) bool { return errors.Is(err, errFDBudget) }

// fdBudget is a ledger of descriptors committed to fs watches. A limit <= 0
// disables accounting entirely (reservations always succeed), which is what
// non-macOS platforms get.
type fdBudget struct {
	mu    sync.Mutex
	limit int
	used  int
}

func newFDBudget(limit int) *fdBudget {
	if limit < 0 {
		limit = 0
	}
	return &fdBudget{limit: limit}
}

// reserve commits n descriptors if they fit. It is all-or-nothing: a refused
// reservation consumes nothing.
func (b *fdBudget) reserve(n int) bool {
	if n <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		b.used += n
		return true
	}
	if b.used+n > b.limit {
		return false
	}
	b.used += n
	return true
}

// release returns n descriptors to the ledger.
func (b *fdBudget) release(n int) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= n
	if b.used < 0 {
		b.used = 0
	}
}

// snapshot returns the committed count and the ceiling (0 == unlimited).
func (b *fdBudget) snapshot() (used, limit int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used, b.limit
}

// ---------------------------------------------------------------------------
// Process-wide default
// ---------------------------------------------------------------------------

var (
	sharedFDBudgetOnce sync.Once
	sharedFDBudgetVal  *fdBudget
)

// sharedFDBudget is the process-wide ledger. Every Watcher constructed without
// an explicit Config.FDBudget shares it, so the ceiling is global across
// subscriptions — which is the point: the kernel limit is per PROCESS, not per
// Watcher and certainly not per repo.
func sharedFDBudget() *fdBudget {
	sharedFDBudgetOnce.Do(func() {
		sharedFDBudgetVal = newFDBudget(defaultFDBudgetLimit())
	})
	return sharedFDBudgetVal
}

// ---------------------------------------------------------------------------
// Exported ledger, for the daemon's OTHER fsnotify consumers (#6233)
// ---------------------------------------------------------------------------
//
// The kernel descriptor limit is per PROCESS. internal/daemon/worktree opens a
// second fsnotify.Watcher for the .git/worktrees directories, drawing from the
// same limit as the subscription watcher; before #6233 it charged nothing, so
// FDBudgetStats reported headroom that a second consumer had already spent.
// These three entry points let that consumer charge the one ledger.
//
// Not accounted here: the fsnotify backend handle itself. A measurement on
// darwin (fsnotify v1.10.1, kqueue backend) showed fsnotify.NewWatcher costing
// 3 descriptors — the kqueue plus a close-notification pipe pair — before any
// Add. Neither watcher charges that: it is a fixed per-Watcher constant, not a
// term that grows with repos or worktrees, and the budget exists to bound the
// growing terms. The two watchers together spend 6 such descriptors, against a
// limit in the tens of thousands.

// ReserveWatchFDs commits n descriptors on the process-wide watch ledger,
// returning false if they do not fit. It is all-or-nothing: a refused
// reservation consumes nothing. Every successful reservation must be paired
// with a ReleaseWatchFDs when the watch is closed.
func ReserveWatchFDs(n int) bool { return sharedFDBudget().reserve(n) }

// ReleaseWatchFDs returns n descriptors previously committed by
// ReserveWatchFDs.
func ReleaseWatchFDs(n int) { sharedFDBudget().release(n) }

// DirWatchFDCost returns the descriptor cost, under this platform's cost
// model, of a single fsnotify watch on one directory that contains entries
// entries. On macOS fsnotify's kqueue backend opens a descriptor for the
// directory and one more for every entry inside it (watchDirectoryFiles);
// elsewhere the per-entry term is zero.
func DirWatchFDCost(entries int) int {
	if entries < 0 {
		entries = 0
	}
	return defaultCostModel().cost(1, entries)
}

// fdBudgetEnv is the operator override. A value <= 0 disables the budget.
const fdBudgetEnv = "GRAFEL_WATCH_FD_BUDGET"

// envFDBudget returns (value, true) when the override is set to a parseable
// integer.
func envFDBudget() (int, bool) {
	v := strings.TrimSpace(os.Getenv(fdBudgetEnv))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	if n < 0 {
		n = 0
	}
	return n, true
}

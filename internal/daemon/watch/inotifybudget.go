package watch

// inotifybudget.go — the inotify budget PROBE (#6932 arm B, part 1).
//
// What this is, and what it deliberately is NOT.
//
// It is a projection: "how many inotify watch descriptors would this
// configuration ask the kernel for, and how many does this host allow". It
// changes no configuration and switches no mode. `auto` still resolves to
// fsnotify, PollingEnabled() is untouched, and a repo subscribes exactly what
// it subscribed before. The only new behaviour is that grafel can now say what
// the watch set costs.
//
// It is NOT an auto-switch. An auto-switch needs a threshold, and every number
// #6932 records is macOS/APFS with no concurrency measurement. This probe is
// what produces the data such a threshold would need, on the target hosts.
//
// The honesty constraint that shapes the whole type:
//
//	fs.inotify.max_user_watches is PER-UID and HOST-LEVEL. It is not
//	namespaced, so every container running as the same UID draws from one
//	pool, and a container cannot raise it. This probe can see grafel's own
//	demand. It cannot see what any other process has already taken out of
//	the same pool. Headroom computed here is therefore an UPPER BOUND on
//	what is actually available, never a promise — and the report says so in
//	its own text rather than leaving the reader to assume otherwise.
//
// On non-Linux the answer is "not applicable on this platform", not 0.
// defaultFDBudgetLimit() returns 0 off Darwin to mean "accounting disabled",
// and a probe that reused that 0 as a limit would read as "no headroom at all"
// on a platform where the question does not arise.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/walk"
)

// inotifyLimitEnv lets an operator supply the ceiling the probe should measure
// against. Two uses: a what-if ("what would my container's 8192 mean for this
// fleet?") from a machine that is not the container, and testability of the
// arithmetic on a host with no /proc. A report built from it names it as the
// source, so an env-supplied number never passes itself off as a kernel read.
const inotifyLimitEnv = "GRAFEL_INOTIFY_MAX_USER_WATCHES"

// InotifyBudget is one probe result.
//
// Dirs is the demand in DIRECTORIES; Projected is that demand converted to
// watch descriptors by the inotify cost model (one wd per directory,
// recursively, nothing per file). The two are kept apart because the directory
// count is a measured property of the tree on any platform, while the
// descriptor figure only means something where an inotify pool exists.
type InotifyBudget struct {
	// Platform is runtime.GOOS, so a report read out of a log or a bug
	// attachment carries the host it describes.
	Platform string
	// PoolApplies is true only where a per-UID inotify watch pool governs
	// the watch set — i.e. Linux, or any host where the operator supplied a
	// ceiling explicitly via inotifyLimitEnv.
	PoolApplies bool
	// Limit is the per-UID ceiling, and 0 means "not known", never "zero
	// watches allowed". Consult LimitKnown rather than testing this field.
	Limit int
	// LimitSource names where Limit came from: the sysctl path, or the env
	// override. Empty when no limit was obtained.
	LimitSource string
	// LimitErr is the reason no limit could be read, if any.
	LimitErr string

	// Repos is how many registered repos the demand covers. Each tracked
	// linked worktree registers as its own repo and subscribes its own tree,
	// so the worktree multiplier is already inside this count rather than
	// being applied to it as arithmetic.
	Repos int
	// Dirs is the number of directories a subscription would take a watch
	// on, summed over Repos.
	Dirs int
	// Projected is Dirs converted to inotify watch descriptors.
	Projected int
	// Measured is true when Dirs is the LIVE subscribed directory set rather
	// than a walk's projection of one. Reported because the two answer
	// slightly different questions and a reader is entitled to know which
	// one they are looking at.
	Measured bool

	// UnwatchedRepos and FailedDirs are the DEGRADED half of the demand, and
	// they exist because a measured figure alone is a LOWER BOUND on demand,
	// not the demand.
	//
	// The watcher's own directory map counts watches the kernel GRANTED. A
	// repo the descriptor budget refused, or a directory whose fsnotify Add
	// failed — on Linux, ENOSPC, which is exactly the inotify pool running
	// out — leaves that map smaller, so the naive measured report gets
	// CHEAPER the worse the failure is, and a repo receiving no events at all
	// renders as "fits". That is the silent degradation this arm exists to
	// expose (#6921, #6923, #6928) reproduced inside the instrument built to
	// expose it, so the refused demand is counted back into Dirs and named
	// here, and Summary says NOT WATCHED out loud.
	UnwatchedRepos int
	FailedDirs     int

	// WhatIf records that Limit came from the operator rather than from this
	// host's kernel. It is carried into the rendered line, not just into the
	// notes: on a platform with no inotify pool at all a bare "N of 8192 —
	// fits" is a Linux-shaped verdict about a host that has no such ceiling.
	WhatIf bool

	// Notes carries the caveats in the report's own words. Every consumer
	// prints them; none of them summarise them away.
	Notes []string
}

// LimitKnown reports whether Limit is a real ceiling this report may be
// compared against.
func (b InotifyBudget) LimitKnown() bool { return b.PoolApplies && b.Limit > 0 }

// WouldExceed reports whether the projected demand does not fit under the
// host's per-UID ceiling — grafel's demand ALONE, before any other process on
// the host is accounted for. With no known limit the answer is false, because
// "we could not read the ceiling" is not evidence of exceeding it.
func (b InotifyBudget) WouldExceed() bool {
	return b.LimitKnown() && b.Projected > b.Limit
}

// Headroom returns how many watch descriptors remain under the ceiling once
// grafel's own demand is met, and whether that number is meaningful at all.
//
// It is an UPPER BOUND. The pool is shared per-UID across every process on the
// host, and this probe cannot see the other consumers, so the true headroom is
// this number minus an unknown.
func (b InotifyBudget) Headroom() (int, bool) {
	if !b.LimitKnown() {
		return 0, false
	}
	return b.Limit - b.Projected, true
}

// demandLabel says how the demand figure was obtained, in the rendered line
// rather than only in a struct field. "measured" for a live subscription,
// "projected" for a walk, and the honest hybrid when a refused repo's cost had
// to be projected back into a measured report.
func (b InotifyBudget) demandLabel() string {
	if !b.Measured {
		return "projected"
	}
	if b.UnwatchedRepos > 0 {
		return fmt.Sprintf("measured, plus a projection of %d refused repo(s)", b.UnwatchedRepos)
	}
	return "measured"
}

// degradedClause is the NOT WATCHED announcement. Empty in the healthy case,
// because a line that always prints trains the reader to ignore it.
func (b InotifyBudget) degradedClause() string {
	if b.UnwatchedRepos == 0 && b.FailedDirs == 0 {
		return ""
	}
	return fmt.Sprintf("; NOT WATCHED: %d repo(s) refused, %d director(ies) the backend rejected — "+
		"their cost IS included above and those trees receive no file events at all, so this is not an all-clear",
		b.UnwatchedRepos, b.FailedDirs)
}

// Summary is the one-line announcement, in the form `grafel status` prints it.
// The caveats live in Notes and are printed with it, never instead of it.
func (b InotifyBudget) Summary() string {
	shape := fmt.Sprintf("(%s, %d dirs across %d repos)", b.demandLabel(), b.Dirs, b.Repos)
	tail := b.degradedClause()

	if !b.PoolApplies {
		return fmt.Sprintf("inotify budget: not applicable on %s — %d dirs across %d repos (%s)%s",
			b.Platform, b.Dirs, b.Repos, b.demandLabel(), tail)
	}
	if !b.LimitKnown() {
		return fmt.Sprintf("inotify budget: %d watch descriptors %s, host limit unknown%s",
			b.Projected, shape, tail)
	}

	verdict := "fits (grafel's demand alone)"
	if free, ok := b.Headroom(); ok && !b.WouldExceed() {
		verdict = fmt.Sprintf("fits (grafel's demand alone), %d left under the ceiling — an UPPER BOUND, the pool is shared", free)
	}
	if b.WouldExceed() {
		verdict = "WOULD EXCEED"
	}

	// A ceiling this host does not have is rendered as the what-if it is. A
	// bare "N of 8192 — fits" on a platform with no inotify pool is a
	// Linux-shaped verdict about a machine that cannot produce one.
	if b.WhatIf && !inotifyPoolApplies {
		return fmt.Sprintf("inotify budget: not applicable on %s (no inotify watch pool on this platform) — "+
			"WHAT-IF against the ceiling supplied in %s: %d of %d watch descriptors %s — %s%s",
			b.Platform, inotifyLimitEnv, b.Projected, b.Limit, shape, verdict, tail)
	}
	return fmt.Sprintf("inotify budget: %d of %d watch descriptors %s — %s%s",
		b.Projected, b.Limit, shape, verdict, tail)
}

// inotifySharedPoolNote is the caveat that must survive every refactor: the
// pool is shared and this probe is half-blind to it.
const inotifySharedPoolNote = "the inotify watch pool is per-UID and host-level, NOT per-container: every process running as this UID draws from the same pool and a container cannot raise it. This probe sees only grafel's own demand — it cannot see what other processes have already taken, so any headroom shown is an upper bound, not a reservation."

// ProjectInotifyBudget projects the watch-descriptor cost of subscribing repos,
// WITHOUT subscribing anything.
//
// extraSkip is the per-Watcher exclude set, so a projection made for a live
// Watcher asks the same question that Watcher's own subscription walk asks.
func ProjectInotifyBudget(repos []string, extraSkip map[string]struct{}) InotifyBudget {
	b := newInotifyBudget()
	b.Repos = len(repos)
	for _, r := range repos {
		b.Dirs += projectWatchDirs(r, extraSkip)
	}
	b.Projected = inotifyCostModel.cost(b.Dirs, 0)
	b.Notes = append(b.Notes, "demand is PROJECTED from a directory walk; nothing was subscribed to produce it.")
	return b
}

// MeasuredInotifyBudget builds a report from a directory count that has already
// been taken — the live subscribed set — rather than from a projection walk.
func MeasuredInotifyBudget(repos, dirs int) InotifyBudget {
	b := newInotifyBudget()
	b.Repos = repos
	b.Dirs = dirs
	b.Measured = true
	b.Projected = inotifyCostModel.cost(dirs, 0)
	b.Notes = append(b.Notes, "demand is the LIVE subscribed directory set — the watches the kernel GRANTED. "+
		"A measured figure ALONE is a lower bound on demand, because a refused repo or a rejected directory makes "+
		"it smaller rather than larger; anything refused is counted back in and called out as NOT WATCHED.")
	return b
}

// newInotifyBudget fills in everything that depends on the host rather than on
// the watch set: the platform, the ceiling, and the caveats attached to both.
func newInotifyBudget() InotifyBudget {
	b := InotifyBudget{Platform: runtime.GOOS}
	if n, ok := envInotifyLimit(); ok {
		// An operator-supplied ceiling makes the comparison meaningful even
		// where the kernel has no such pool — as a what-if. It is labelled as
		// such so it cannot be mistaken for a reading of this host.
		b.PoolApplies = true
		b.WhatIf = true
		b.Limit = n
		b.LimitSource = inotifyLimitEnv
		b.Notes = append(b.Notes, fmt.Sprintf("limit was supplied by %s, NOT read from this host's kernel.", inotifyLimitEnv))
		if !inotifyPoolApplies {
			b.Notes = append(b.Notes, fmt.Sprintf("%s has no inotify watch pool of its own; this is a what-if against a Linux ceiling.", runtime.GOOS))
		}
		b.Notes = append(b.Notes, inotifySharedPoolNote)
		return b
	}
	if !inotifyPoolApplies {
		b.Notes = append(b.Notes,
			fmt.Sprintf("inotify is a Linux facility and there is no per-UID watch pool on %s, so no limit applies here. The directory count is still real: it is what a Linux host would be asked for. Set %s to compare it against a ceiling.", runtime.GOOS, inotifyLimitEnv))
		return b
	}
	b.PoolApplies = true
	n, src, err := readInotifyLimit()
	b.LimitSource = src
	if err != nil {
		b.LimitErr = err.Error()
		b.Notes = append(b.Notes, fmt.Sprintf("could not read %s (%v), so the ceiling is unknown and nothing here is compared against one.", src, err))
	} else {
		b.Limit = n
	}
	b.Notes = append(b.Notes, inotifySharedPoolNote)
	return b
}

// envInotifyLimit returns (value, true) when the operator override is set to a
// parseable non-negative integer.
func envInotifyLimit() (int, bool) {
	v := strings.TrimSpace(os.Getenv(inotifyLimitEnv))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// projectWatchDirs counts the directories a subscription of repoPath would take
// a watch on.
//
// It mirrors subscribeRepo's walk in the ONE respect that decides the count:
// which directories survive. The per-directory verdict is not re-implemented
// here — watchDirSkip is the single definition of it and both walks call it —
// and the two remaining gates (the TCC protected-path guard and the watch-dir
// cap) are the same helpers applied in the same order. The projection is
// checked against a real subscription's count by
// TestProjectInotifyBudget_MatchesWhatSubscriptionActuallyTakes; a projection
// nobody compared to a measurement is arithmetic, not a probe.
func projectWatchDirs(repoPath string, extraSkip map[string]struct{}) int {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return 0
	}
	if protected, _ := walk.IsProtectedPath(abs); protected {
		return 0
	}
	dirCap := walk.WatchDirCap()
	added := 0
	_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if protected, _ := walk.IsProtectedPath(p); protected {
			return filepath.SkipDir
		}
		if dirCap > 0 && added >= dirCap {
			return filepath.SkipDir
		}
		if skip, _ := watchDirSkip(abs, p, extraSkip); skip != watchSkipNone {
			return filepath.SkipDir
		}
		added++
		return nil
	})
	return added
}

// inotifyProbeTTL is how long a budget report is reused before the walk is
// repeated. A variable so tests can drive the refresh path without sleeping.
var inotifyProbeTTL = 60 * time.Second

// InotifyBudget reports what this Watcher's configuration would cost the host's
// per-UID inotify watch pool (#6932 arm B). It subscribes nothing, unsubscribes
// nothing, and switches no mode.
//
// Which question it answers depends on what is live:
//
//   - fsnotify mode: the demand is MEASURED. The subscribed directory set IS
//     the demand, and the watcher already knows its size, so no walk happens.
//   - poll mode (a change-detection delegate is installed): nothing is
//     subscribed at all, so the demand is PROJECTED by walking the registered
//     repos. That is the number worth having there — it is what poll mode is
//     not spending.
//
// The result is cached for inotifyProbeTTL because the projection walk is not
// free and /diagnostics is polled.
func (w *Watcher) InotifyBudget() InotifyBudget {
	w.inotifyProbeMu.Lock()
	defer w.inotifyProbeMu.Unlock()
	now := w.clk.Now()
	if !w.inotifyProbeAt.IsZero() && now.Sub(w.inotifyProbeAt) < inotifyProbeTTL {
		return w.inotifyProbeVal
	}
	var b InotifyBudget
	if w.cfg.Delegate != nil {
		b = ProjectInotifyBudget(w.Repos(), w.extraSkip)
		b.Notes = append(b.Notes, "change_detection=poll: grafel holds ZERO inotify watch descriptors right now. The figure above is what fsnotify mode would ask this host for instead.")
	} else {
		repos, dirs, _, _, _ := w.Stats()
		b = MeasuredInotifyBudget(repos, dirs)
		// The demand is what was ATTEMPTED, not what survived. Two ways a
		// watch is attempted and not granted, and both make the measured set
		// SMALLER — which is why reporting the measured set alone announces
		// "fits" exactly when the watcher has been refused:
		//
		//  1. A repo the descriptor budget refused outright. It is registered
		//     with the daemon, receives no events, and is absent from
		//     w.dirToRepo. Its cost is projected back in.
		//  2. A directory whose fsnotify Add failed — on Linux, ENOSPC from a
		//     max_user_watches pool that is already full. subscribeRepo logs
		//     WARN and carries on, so the shortfall is exactly the number of
		//     watches the kernel would not give us.
		if _, _, _, refused := w.FDBudgetStats(); len(refused) > 0 {
			p := ProjectInotifyBudget(refused, w.extraSkip)
			b.Repos += p.Repos
			b.Dirs += p.Dirs
			b.UnwatchedRepos = len(refused)
		}
		if n := atomic.LoadUint64(&w.fsAddFailed); n > 0 {
			b.FailedDirs = int(n)
			b.Dirs += int(n)
		}
		b.Projected = inotifyCostModel.cost(b.Dirs, 0)
		if b.UnwatchedRepos > 0 || b.FailedDirs > 0 {
			b.Notes = append(b.Notes, fmt.Sprintf(
				"DEGRADED: %d repo(s) were refused and %d director(ies) were rejected by the backend. "+
					"Their cost is included in the demand above, but those trees are receiving no file "+
					"events — a re-index will not be triggered by edits there.",
				b.UnwatchedRepos, b.FailedDirs))
		}
	}
	w.inotifyProbeVal = b
	w.inotifyProbeAt = now
	return b
}

// InotifyBudgetReport is InotifyBudget flattened to primitives: the one-line
// announcement, its caveats, and whether grafel's own demand exceeds the host
// ceiling. It exists so the status and /diagnostics surfaces can report the
// probe without importing this package's types — and so the caveats travel WITH
// the summary rather than being reconstructed downstream by a caller that
// cannot know how the number was obtained.
func (w *Watcher) InotifyBudgetReport() (summary string, notes []string, exceeds bool) {
	b := w.InotifyBudget()
	return b.Summary(), b.Notes, b.WouldExceed()
}

// Package watchscan finds and reaps stale-version / orphaned standalone
// `grafel watch <repo>` processes for the daemon's MANAGED repos (issue #5632).
//
// # Problem
//
// A standalone `grafel watch <repo>` process is launched by an OS unit
// (launchd/systemd/schtasks) whose exec line is `<BinPath> watch <repo>`. If a
// stale `go install` build still has a unit registered (a `$GOPATH/bin/grafel`
// from an older version), or a watcher was started by hand, the daemon can end
// up with a watcher running from an OUT-OF-DATE binary alongside the installed
// daemon — version skew. Orphan watchers reparented to init also survive daemon
// restarts. The existing watchreg-based reaper (#5142) only reaps watchers that
// SELF-REGISTERED in watchers.json, and only on PID-liveness / owner mismatch —
// it never compares the watcher's EXECUTABLE to the daemon's own, so a
// foreign-binary watcher whose owner stamp happens to match (or that never
// registered) is invisible to it.
//
// # Design
//
// watchscan is the executable-aware complement to the watchreg sweep. It
// enumerates live `grafel watch <repo>` processes (via an injectable lister so
// the decision logic is unit-testable with no real processes), and for each one
// targeting a repo the daemon MANAGES it decides whether to reap it:
//
//   - a watcher whose executable path differs from the daemon's own
//     os.Executable() is a stale/foreign-version watcher → reap; and
//   - among watchers for the SAME managed repo, keep exactly one (the
//     daemon's-own-exe one if present, else the lowest PID) and reap the rest.
//
// It NEVER touches a process for a repo the daemon does not manage, and it is
// strictly best-effort: a lister error (or a platform that cannot enumerate)
// yields an empty plan, so the daemon is never destabilized by enumeration
// failure.
package watchscan

import (
	"path/filepath"
	"sort"
)

// Proc is one live `grafel watch <repo>` process the lister found.
type Proc struct {
	// PID is the watcher process id.
	PID int
	// Exe is the absolute path to the executable backing the process, when the
	// platform could resolve it. May be empty if unresolved.
	Exe string
	// Repo is the absolute repo path the watcher targets (its `watch <repo>`
	// argument), normalized to an absolute path by the lister when possible.
	Repo string
}

// Deps are the injectable primitives the scan needs.
type Deps struct {
	// List returns the currently-live `grafel watch <repo>` processes. A nil
	// List, or one that errors, makes Plan return an empty plan (best-effort).
	List func() ([]Proc, error)
	// SelfExe is the daemon's own executable path (os.Executable() in
	// production). A watcher whose Exe differs from this — when both are known —
	// is treated as a stale/foreign-version watcher.
	SelfExe string
	// Managed reports whether repoPath is a repo the daemon manages. Only
	// watchers for managed repos are ever reaped. Required; a nil Managed makes
	// the plan empty (nothing is considered managed).
	Managed func(repoPath string) bool
}

// Plan is the set of PIDs that should be reaped.
type Plan struct {
	// Foreign are PIDs reaped because their executable differs from the daemon's
	// own (version skew / orphan from a different install).
	Foreign []int
	// Duplicate are PIDs reaped because another watcher already covers the same
	// managed repo (one-watcher-per-repo).
	Duplicate []int
}

// PIDs returns every PID the plan would reap (foreign + duplicate), sorted and
// de-duplicated.
func (p Plan) PIDs() []int {
	seen := map[int]struct{}{}
	var out []int
	for _, pid := range p.Foreign {
		if _, ok := seen[pid]; !ok {
			seen[pid] = struct{}{}
			out = append(out, pid)
		}
	}
	for _, pid := range p.Duplicate {
		if _, ok := seen[pid]; !ok {
			seen[pid] = struct{}{}
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out
}

// sameExe reports whether two executable paths refer to the same binary. Both
// must be non-empty to compare; an unknown path (empty) is never declared a
// mismatch, so we don't reap a watcher merely because we couldn't read its exe.
func sameExe(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// Compute computes which managed-repo watchers to reap. It is pure (no process
// side effects): the caller terminates the returned PIDs. Determinism: within a
// repo, watchers are considered in ascending PID order, and the kept survivor is
// the daemon's-own-exe watcher if one exists, otherwise the lowest PID.
func Compute(deps Deps) Plan {
	var plan Plan
	if deps.List == nil || deps.Managed == nil {
		return plan
	}
	procs, err := deps.List()
	if err != nil {
		return plan // best-effort: enumeration failure → reap nothing.
	}

	// Group managed-repo watchers by normalized repo path.
	byRepo := map[string][]Proc{}
	for _, p := range procs {
		if p.PID <= 0 || p.Repo == "" {
			continue
		}
		repo := filepath.Clean(p.Repo)
		if !deps.Managed(repo) {
			continue // never touch a watcher for an unmanaged repo.
		}
		byRepo[repo] = append(byRepo[repo], p)
	}

	// Deterministic repo iteration order for stable output.
	repos := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	for _, repo := range repos {
		group := byRepo[repo]
		sort.Slice(group, func(i, j int) bool { return group[i].PID < group[j].PID })

		// First, flag every foreign-exe watcher for reaping.
		foreign := map[int]bool{}
		for _, p := range group {
			if !sameExe(p.Exe, deps.SelfExe) {
				plan.Foreign = append(plan.Foreign, p.PID)
				foreign[p.PID] = true
			}
		}

		// Among the survivors (own-exe / unknown-exe watchers), keep exactly one
		// and reap the rest as duplicates. Prefer a watcher whose exe matches the
		// daemon's own; otherwise the lowest PID (group is PID-sorted).
		var survivors []Proc
		for _, p := range group {
			if !foreign[p.PID] {
				survivors = append(survivors, p)
			}
		}
		if len(survivors) <= 1 {
			continue
		}
		keep := pickSurvivor(survivors, deps.SelfExe)
		for _, p := range survivors {
			if p.PID != keep {
				plan.Duplicate = append(plan.Duplicate, p.PID)
			}
		}
	}
	return plan
}

// pickSurvivor returns the PID to keep among same-repo survivor watchers:
// the one whose exe matches the daemon's own if present, else the lowest PID.
// survivors is assumed PID-sorted ascending.
func pickSurvivor(survivors []Proc, selfExe string) int {
	if selfExe != "" {
		for _, p := range survivors {
			if p.Exe != "" && filepath.Clean(p.Exe) == filepath.Clean(selfExe) {
				return p.PID
			}
		}
	}
	return survivors[0].PID
}

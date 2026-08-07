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
//   - a watcher whose executable is PROVABLY a different binary from the
//     daemon's own os.Executable() is a stale/foreign-version watcher → reap.
//     "Provably" is load-bearing: the comparison resolves symlinks and treats
//     any path it cannot resolve as unknown rather than as skew, because a
//     false foreign verdict silently and permanently kills the user's watchers
//     (#6187, and see #6179 for why it stopped being self-healing); and
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
	// production). A watcher whose Exe is PROVABLY a different binary from this
	// — both paths known, both resolving on disk, resolving to different files
	// — is treated as a stale/foreign-version watcher. Symlinks are resolved,
	// so a shim invocation is not skew; an unresolvable path on either side is
	// not skew either. See sameExe (#6187).
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

// sameExe reports whether two executable paths MIGHT refer to the same binary
// — i.e. whether reaping is forbidden. It is deliberately asymmetric: it
// returns false (→ reap as foreign) only when the two paths are PROVABLY
// different binaries, and true in every case where the answer cannot be
// established.
//
// # Why a lexical comparison was wrong (#6187)
//
// This used to be filepath.Clean(a) == filepath.Clean(b). SelfExe is the
// engine's os.Executable() and the watcher's exe is its plist's BinPath, and
// those two spellings routinely differ while naming ONE binary: grafel invoked
// through a Homebrew shim, through any symlink on PATH, or with a BinPath
// recorded from an install prefix that is now a symlink to the real one. Every
// such install had EVERY launchd watcher for EVERY managed repo classified
// foreign and SIGTERMed on the 5-minute sweep. Before #6179 that was a loud
// reap↔respawn oscillation; after it the watcher exits 0, launchd leaves it
// dead forever, and the only trace is one line in the repo's watcher.err.log.
// Resolving symlinks is what stops a shim from looking like version skew.
//
// # Why an unresolvable path means "not foreign"
//
// filepath.EvalSymlinks fails outright on a path that does not exist, and "the
// recorded exe no longer exists" is an ORDINARY state here — a stale plist
// whose BinPath pointed into a prefix since removed, an upgrade that replaced
// the tree rather than the file, a binary deleted out from under a running
// process. resolveDeepestExisting (internal/registry, #6194) is the sibling
// answer to the same EvalSymlinks trap, but it does not fit: it resolves the
// deepest existing ANCESTOR and re-appends the missing tail so a path that does
// not exist yet stays comparable. The missing tail here IS the binary, and two
// prefixes that resolve identically tell us nothing about whether the two
// binaries were the same file. Re-appending would manufacture a verdict from
// directory names alone — including a FOREIGN verdict, which is the one that
// costs the user their watchers.
//
// So the two verdicts are not symmetric in consequence, and the code follows
// the consequence:
//
//   - a wrong "foreign" is permanent and silent: the watcher is killed, launchd
//     does not bring it back, and nothing tells the user;
//   - a wrong "same" leaves a stale-version watcher running, which is
//     recoverable, visible in `grafel doctor`, and — for the case that actually
//     matters, two watchers on one repo — still collapsed by the duplicate
//     rule below, which prefers the watcher we can vouch for.
//
// Hence: unknown (empty), or either side unresolvable, or resolving to the same
// file → same. Only two paths that BOTH resolve, to different files, are skew.
func sameExe(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, okA := resolveExe(a)
	if !okA {
		return true // unresolvable → unknowable → never reap on this basis.
	}
	rb, okB := resolveExe(b)
	if !okB {
		return true
	}
	return ra == rb
}

// definitelySameExe is sameExe's strict converse: it reports whether the two
// paths are PROVABLY the same binary. Where sameExe answers "may I reap this?",
// this answers "can I vouch for this one?" — and an unknown must not count as a
// vouch, or the duplicate rule would keep a watcher whose executable it could
// not read in preference to one it could.
func definitelySameExe(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, okA := resolveExe(a)
	if !okA {
		return false
	}
	rb, okB := resolveExe(b)
	if !okB {
		return false
	}
	return ra == rb
}

// resolveExe returns p's symlink-resolved, cleaned form, and whether it
// resolved at all. A path that does not exist does not resolve, and that is
// reported rather than papered over so both callers above can apply their own
// fail-safe direction to it.
func resolveExe(p string) (string, bool) {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	return filepath.Clean(r), true
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
	// definitelySameExe, not a Clean comparison: when the daemon was invoked
	// through a shim the own-exe watcher spells its path differently, and a
	// lexical check would fail to recognise it and fall through to the lowest
	// PID — keeping an arbitrary watcher and reaping the one we can vouch for
	// (#6187).
	if selfExe != "" {
		for _, p := range survivors {
			if definitelySameExe(p.Exe, selfExe) {
				return p.PID
			}
		}
	}
	return survivors[0].PID
}

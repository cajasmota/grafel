package install

// watchersync.go implements ReconcileWatcherUnits: bring already-installed
// per-repo watcher units on disk into agreement with the CURRENT binary's unit
// template, and re-register only the ones whose content actually changed.
//
// ── Why this exists (issue #6179 F1) ─────────────────────────────────────────
//
// #6179 fixed the generated LaunchAgent (conditional KeepAlive, a
// ThrottleInterval) and the exit statuses that make the conditional mean
// something. But a template fix reaches nobody who is already affected unless
// something rewrites the units that are already on disk, and before this file
// nothing on any supported upgrade path did:
//
//   - watchers.Write is reached ONLY from Apply's per-repo loop.
//   - `grafel update` calls Apply from internal/cli/update.go BEFORE
//     runSelfUpdate swaps the binary, so that pass renders the OLD template;
//     the post-swap step is RunCopy, which never touches watcher units.
//   - The curl installer runs `grafel install --refresh-state`, which
//     short-circuits to RefreshState — it records the binary and does nothing
//     else.
//
// So the reporter, at 140 repos, could upgrade by either supported path and end
// up with 140 plists still saying `<key>KeepAlive</key><true/>` with no
// ThrottleInterval, still loaded, still storming.
//
// ── Why it is a content diff, and why it only touches existing units ─────────
//
// The obvious repair — re-run Apply for every group — re-registers every unit,
// and each registration is a launchctl bootout+bootstrap that macOS Ventura+
// posts a Background Items notification for. That burst across 140 repos is
// itself part of what #6179 reports. Two constraints follow:
//
//  1. Render first, compare to the bytes on disk, and do NOTHING when they
//     match. On a machine already carrying the current template this is a pure
//     read: zero writes, zero launchctl calls, zero notifications. That makes
//     it safe to call unconditionally from every upgrade path, and it means the
//     one-time repair costs exactly one registration per genuinely stale unit.
//
//  2. Only reconcile units that ALREADY EXIST on disk. A repo with no unit file
//     has no stale plist and is not storming, so creating one here would be
//     new registrations for no benefit — and it would silently resurrect units
//     a user deliberately booted out. Installing missing units remains Apply's
//     job, where the user asked for it.
//
//     One narrow exception was added with #6183: a repo whose colliding sibling
//     still has a unit. Those two repos shared ONE file under the old label, so
//     "this repo has no unit" is a fact the collision itself manufactured, not
//     a choice the user made. See planWatcherUnits.
//
// ── What this does NOT reap (#6183 F3) ───────────────────────────────────────
//
// Orphaned units are handled only where the label is derivable from the
// registry — that is, for repos still listed in a group config. Two cases are
// deliberately left alone, and neither is fixed by this file:
//
//   - A repo REMOVED from a group config. Its units (both labels) stay on disk
//     and stay loaded. Reaping them would mean enumerating the unit directory
//     and deleting whatever does not correspond to a registered repo — which
//     cannot distinguish "the user removed this repo" from "this group's config
//     is temporarily unreadable, or belongs to a checkout this binary cannot
//     see", and would delete a live watcher in the second case. The label
//     contains the group name verbatim, and group names may contain dots, so
//     even parsing a label back into (group, repo) is not sound.
//
//   - Units for groups that are not in this binary's registry at all.
//
// `grafel uninstall <group>` reaps a group's units while it can still name
// them. Removing a single repo from a config does not, today, reap that repo's
// units. That is a pre-existing gap that #6183 narrows (by handling the
// watchers-disabled group) but does not close.

import (
	"fmt"
	"os"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// newWatcherLoader is the Loader constructor ReconcileWatcherUnits uses. It is
// a package var so tests can count Load calls without shelling out to
// launchctl/systemctl/schtasks.
var newWatcherLoader = watchers.NewLoader

// ReconcileWatcherOptions configures ReconcileWatcherUnits.
type ReconcileWatcherOptions struct {
	// BinPath is the grafel binary the units should invoke. Defaults to
	// os.Executable(). It participates in the rendered body, so a changed
	// BinPath is itself a reason to rewrite.
	BinPath string
}

// ReconcileWatcherResult reports what ReconcileWatcherUnits did.
type ReconcileWatcherResult struct {
	// Examined counts units considered (registered repo, watchers enabled).
	Examined int
	// Current counts units whose on-disk bytes already matched the template.
	// These are NOT rewritten and NOT re-registered.
	Current int
	// Absent counts repos with no unit file on disk — under either the current
	// or the pre-#6183 label. Left alone by design.
	Absent int
	// Migrated lists the pre-#6183 unit paths that were booted out of the OS
	// scheduler and deleted because the repo's label changed (#6183). Each
	// entry is an OLD path that no longer exists; the replacement appears in
	// Rewritten.
	Migrated []string
	// Rewritten lists the unit paths whose content was stale and replaced.
	Rewritten []string
	// Reloaded lists the unit paths successfully re-registered with the OS.
	Reloaded []string
	// Warnings collects non-fatal per-unit failures. Reconciliation never
	// aborts on one bad unit: the remaining 139 still need repairing.
	Warnings []string
}

// watcherPlan is one repo's probed state: both label derivations, and whether a
// unit exists under either. Purely observational — building a plan performs no
// writes and constructs no Loader, which is what keeps an up-to-date machine a
// pure read (#6179).
type watcherPlan struct {
	unit         watchers.Unit
	path         string // unit path under the current label
	legacyPath   string // unit path under the pre-#6183 label
	exists       bool   // a unit file exists at path
	legacyExists bool   // a unit file exists at legacyPath
	body         []byte // contents of path, when it exists
	statErr      error  // the ReadFile error (may be os.ErrNotExist)
	readErr      error  // a REAL read failure, worth warning about
}

// planWatcherUnits probes every repo in a group before anything is mutated.
//
// Deriving all the paths up front is what lets the collision recovery survive a
// crash: colliding repos share one legacy path, and once the first member
// retires it the shared file is gone, so the shared-ness has to be read from the
// registry rather than from the filesystem or from a memo of what this process
// already did.
//
// Repos whose unit path cannot be derived at all are warned about and dropped.
func planWatcherUnits(group string, cfg *registry.GroupConfig, binPath string, res *ReconcileWatcherResult) []watcherPlan {
	plans := make([]watcherPlan, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		u := watchers.Unit{Group: group, Repo: r.Path, BinPath: binPath}
		path, err := watchers.UnitPath(u)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", r.Path, err))
			continue
		}
		pl := watcherPlan{unit: u, path: path}

		legacyPath, lerr := watchers.LegacyUnitPath(u)
		if lerr == nil && legacyPath != path {
			pl.legacyPath = legacyPath
			if _, serr := os.Stat(legacyPath); serr == nil {
				pl.legacyExists = true
			}
		}

		body, rerr := os.ReadFile(path)
		pl.statErr = rerr
		switch {
		case rerr == nil:
			pl.exists = true
			pl.body = body
		case os.IsNotExist(rerr):
			// Absent is a normal state, not a failure.
		default:
			pl.readErr = rerr
		}
		plans = append(plans, pl)
	}
	return plans
}

// ReconcileWatcherUnits rewrites and re-registers every already-installed
// watcher unit whose on-disk content differs from what this binary renders.
//
// It is safe to call unconditionally and is a no-op on an up-to-date machine.
// Errors from individual units are collected as warnings rather than returned:
// a single unwritable plist must not stop the other repos from being repaired.
// A non-nil error is returned only when the registry itself cannot be read.
func ReconcileWatcherUnits(opts ReconcileWatcherOptions) (*ReconcileWatcherResult, error) {
	if opts.BinPath == "" {
		bin, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve binary path: %w", err)
		}
		opts.BinPath = bin
	}

	refs, err := registry.Groups()
	if err != nil {
		return nil, fmt.Errorf("read group registry: %w", err)
	}

	res := &ReconcileWatcherResult{}
	var loader watchers.Loader // constructed lazily: an up-to-date machine needs none
	lazyLoader := func() watchers.Loader {
		if loader == nil {
			loader = newWatcherLoader()
		}
		return loader
	}

	for _, ref := range refs {
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("group %s: %v", ref.Name, err))
			continue
		}

		// ── Phase 1: probe, decide nothing ───────────────────────────────────
		//
		// Every path derivation and every stat happens before the first
		// mutation, and that ordering is load-bearing (#6183). Repos that
		// collided under the pre-#6183 label SHARED one unit file, so the
		// moment the first of them retires it the rest become
		// indistinguishable from repos that never had a unit — and reconcile
		// leaves those alone. Probing first means every member of a colliding
		// set has already recorded legacyExists=true from the one file, so each
		// gets its own unit no matter which order they are processed in. A
		// single fused loop that re-stats as it goes silently watches only the
		// first member, which is #6183's original symptom.
		//
		// Nothing here is remembered ACROSS calls, deliberately. A pass killed
		// partway through a colliding set leaves the survivor with no unit
		// under either label, and the next reconcile reports it Absent —
		// printReconcileSummary names it and points at `grafel install`. That
		// is the whole recovery, and it has to be, because "interrupted
		// mid-migration" and "the user booted this watcher out on purpose" are
		// byte-for-byte identical on disk: a sibling with a current unit, this
		// repo with none, no legacy unit anywhere. An earlier revision tried to
		// re-create the survivor automatically by treating a sibling's
		// CURRENT-label unit as evidence of a just-retired shared unit. That
		// evidence never expires, so every colliding repo whose unit a user had
		// deliberately deleted was re-created and re-bootstrapped on every
		// reconcile, forever — the #6183 non-convergence signature, restored,
		// and gated on nothing more meaningful than two repos sharing a
		// directory name. Reconcile does not guess at intent.
		plans := planWatcherUnits(ref.Name, cfg, opts.BinPath, res)

		// ── Phase 2: act ─────────────────────────────────────────────────────
		for _, pl := range plans {
			u := pl.unit

			// A group whose watchers are switched off used to be skipped
			// wholesale, before any repo was looked at. That left a pre-#6183
			// unit on disk AND loaded for a repo that is still registered and
			// whose legacy label is therefore still perfectly derivable — and
			// #6183 turned that from harmless into harmful: re-enabling
			// watchers would install the NEW label alongside the old one
			// instead of overwriting it, giving the repo two watchers. Retire
			// it. Install nothing: watchers are off, and that is the whole
			// meaning of the flag.
			if !cfg.Features.Watchers {
				if pl.legacyExists {
					if removed, merr := watchers.MigrateLegacyUnit(u, lazyLoader); merr != nil {
						res.Warnings = append(res.Warnings,
							fmt.Sprintf("retire legacy unit %s: %v", pl.legacyPath, merr))
					} else if removed != "" {
						res.Migrated = append(res.Migrated, removed)
					}
				}
				continue
			}

			if !pl.exists && !pl.legacyExists {
				// No unit under either label, and no colliding sibling that
				// proves one used to cover this repo — not stale, not storming,
				// and not something to create: installing missing units is
				// Apply's job, where the user asked for it.
				res.Absent++
				continue
			}
			res.Examined++
			if pl.readErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", pl.path, pl.readErr))
				continue
			}

			path := pl.path
			legacyPath := pl.legacyPath
			existing := pl.body
			err := pl.statErr

			// Retire the old-label unit before anything registers the new one,
			// so the repo is never bootstrapped twice. This is what makes the
			// label change a MOVE rather than a duplication.
			if pl.legacyExists {
				removed, merr := watchers.MigrateLegacyUnit(u, lazyLoader)
				if merr != nil {
					res.Warnings = append(res.Warnings,
						fmt.Sprintf("retire legacy unit %s: %v", legacyPath, merr))
					continue
				}
				if removed != "" {
					res.Migrated = append(res.Migrated, removed)
				}
			}

			// Checked AFTER the migration on purpose: if a previous run already
			// wrote the new unit and something re-created the old file, the new
			// one still needs no rewrite and no re-registration — only the
			// duplicate had to go. Running this function twice is then a no-op.
			if err == nil && pl.exists && string(existing) == watchers.Render(u) {
				res.Current++
				continue
			}

			if _, err := watchers.Write(u); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("rewrite %s: %v", path, err))
				continue
			}
			res.Rewritten = append(res.Rewritten, path)

			// Clear the crash-loop history BEFORE re-registering (#6179 F4-a).
			// Load performs a bootout+bootstrap, and the resulting launch is
			// itself a counted start; leaving an over-threshold history in
			// place would make the freshly registered watcher give up on its
			// first tick. Only re-registered units are reset — clearing every
			// repo's history on every upgrade would quietly disarm the
			// detector fleet-wide.
			_ = watchers.ResetWatchStarts(u.Repo)

			// Only now does the OS need telling. A rewritten plist has no
			// effect until the job is re-bootstrapped — launchd holds the
			// settings it read at load time.
			if err := lazyLoader().Load(u); err != nil && !watchers.IsNonFatal(err) {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("re-register %s: %v (the file is updated; it takes effect at next login)", path, err))
				continue
			}
			res.Reloaded = append(res.Reloaded, path)
		}
	}
	return res, nil
}

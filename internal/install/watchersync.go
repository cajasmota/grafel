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
	// Absent counts repos with no unit file on disk. Left alone by design.
	Absent int
	// Rewritten lists the unit paths whose content was stale and replaced.
	Rewritten []string
	// Reloaded lists the unit paths successfully re-registered with the OS.
	Reloaded []string
	// Warnings collects non-fatal per-unit failures. Reconciliation never
	// aborts on one bad unit: the remaining 139 still need repairing.
	Warnings []string
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

	for _, ref := range refs {
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("group %s: %v", ref.Name, err))
			continue
		}
		if !cfg.Features.Watchers {
			continue
		}
		for _, r := range cfg.Repos {
			u := watchers.Unit{Group: ref.Name, Repo: r.Path, BinPath: opts.BinPath}
			path, err := watchers.UnitPath(u)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", r.Path, err))
				continue
			}
			existing, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				// No unit installed for this repo — not stale, not storming.
				res.Absent++
				continue
			}
			res.Examined++
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			if string(existing) == watchers.Render(u) {
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
			_ = watchers.ResetWatchStarts(r.Path)

			// Only now does the OS need telling. A rewritten plist has no
			// effect until the job is re-bootstrapped — launchd holds the
			// settings it read at load time.
			if loader == nil {
				loader = newWatcherLoader()
			}
			if err := loader.Load(u); err != nil && !watchers.IsNonFatal(err) {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("re-register %s: %v (the file is updated; it takes effect at next login)", path, err))
				continue
			}
			res.Reloaded = append(res.Reloaded, path)
		}
	}
	return res, nil
}

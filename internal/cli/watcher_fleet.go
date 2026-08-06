package cli

// watcher_fleet.go — fleet-wide control of the PER-REPO watcher units.
//
// ── What was actually wrong ──────────────────────────────────────────────────
//
// `grafel install` writes one OS unit per registered repo
// (internal/install/install.go, watchers.Write + Loader.Load): a launchd
// LaunchAgent `com.grafel.watcher.<group>.<slug>` on macOS, a systemd --user
// service on Linux, a scheduled task on Windows. Each one runs
// `grafel watch <repo>`, which polls the repo and enqueues index work on its
// own ~30s schedule.
//
// Those units are owned by the OS service manager, NOT by the daemon. Nothing
// about stopping the daemon stops them; on macOS KeepAlive respawns any that
// exit. `grafel stop` (runDaemonStop) only ever stopped the DAEMON's own
// service — #6044 made that stop service-aware and persistent, but only for
// the one `com.grafel.daemon` label. On a 140-repo machine the other 140
// labels carried right on indexing after a "daemon stopped" success message.
//
// ── The contract this file implements ────────────────────────────────────────
//
//	stop  → every INSTALLED watcher unit is deactivated, whatever the group's
//	        features.watchers flag says (that flag is not retroactive: units
//	        written by an earlier install survive it), and whatever fails is
//	        NAMED on stdout. Silence is what produced the report.
//	start → every installed unit belonging to a group with features.watchers
//	        enabled is reactivated. Start is gated on the flag precisely
//	        because stop is not: an unconditional restore would resurrect
//	        watchers the user deliberately turned off.
//
// Neither direction ever CREATES or DELETES a unit file. Installing units is
// `grafel install`'s job; removing them is `grafel uninstall`/group-delete's.
// Stop/start only flip activation, so the pair is losslessly reversible.
//
// ── Persistence ──────────────────────────────────────────────────────────────
//
// The stop is PERSISTENT across reboot/login, matching the daemon stop that
// #6044 established ("will not restart automatically — even across reboot —
// until 'grafel start'"). A session-only watcher stop would mean `grafel stop`
// changes meaning at the next login, and a user who stops grafel to get their
// laptop back does not expect it to come back by itself. The persistence lives
// in each platform Loader.Unload: systemd `disable --now` and the schtasks
// delete were already persistent; the darwin loader's bare `bootout` was not,
// and now pairs with `launchctl disable` (see loader_darwin.go).

import (
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// newFleetWatcherLoader constructs the platform Loader used for fleet-wide
// activation changes. A package var so tests can substitute a fake and never
// shell out to a real launchctl/systemctl/schtasks.
var newFleetWatcherLoader = watchers.NewLoader

// fleetWatcherConcurrency bounds how many units are acted on in parallel. Each
// action is one-to-three short exec calls; at 140 repos a strictly serial sweep
// costs several seconds of wall clock on a command a user expects to be quick.
// It is kept small because the underlying service managers serialise internally
// anyway and a wide fan-out buys nothing.
const fleetWatcherConcurrency = 8

// fleetWatcherResult reports the outcome of one fleet-wide sweep.
type fleetWatcherResult struct {
	// Total counts units that were installed on disk and therefore acted on.
	Total int
	// Changed counts units whose activation change succeeded.
	Changed int
	// Failures holds one "<repo>: <error>" line per unit that did not change.
	// Sorted, so output is deterministic.
	Failures []string
}

// fleetWatcherUnit pairs a unit with its on-disk path.
type fleetWatcherUnit struct {
	unit watchers.Unit
	path string
}

// installedFleetWatcherUnits enumerates every registered repo's watcher unit
// that actually exists on disk.
//
// onlyEnabled restricts the sweep to groups whose features.watchers is on.
// Pass false for stop (a unit on disk is running regardless of what the config
// currently says) and true for start (never resurrect what the user disabled).
//
// A group whose config cannot be read is skipped with a warning rather than
// aborting: one bad fleet.json must not leave the other 139 watchers running.
func installedFleetWatcherUnits(onlyEnabled bool) ([]fleetWatcherUnit, []string, error) {
	refs, err := registry.Groups()
	if err != nil {
		return nil, nil, fmt.Errorf("read group registry: %w", err)
	}
	bin, _ := os.Executable() // best effort; Label/UnitPath do not depend on it

	var units []fleetWatcherUnit
	var warnings []string
	for _, ref := range refs {
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("group %s: %v", ref.Name, err))
			continue
		}
		if onlyEnabled && !cfg.Features.Watchers {
			continue
		}
		for _, r := range cfg.Repos {
			u := watchers.Unit{Group: ref.Name, Repo: r.Path, BinPath: bin}
			path, err := watchers.UnitPath(u)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", r.Path, err))
				continue
			}
			if _, err := os.Stat(path); err != nil {
				// No unit installed for this repo — nothing to activate or
				// deactivate. Not an error and not a warning.
				continue
			}
			units = append(units, fleetWatcherUnit{unit: u, path: path})
		}
	}
	return units, warnings, nil
}

// sweepFleetWatchers applies act to every enumerated unit, bounded by
// fleetWatcherConcurrency, and collects failures.
func sweepFleetWatchers(units []fleetWatcherUnit, act func(watchers.Unit) error) fleetWatcherResult {
	res := fleetWatcherResult{Total: len(units)}
	if len(units) == 0 {
		return res
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, fleetWatcherConcurrency)
	)
	for _, fu := range units {
		wg.Add(1)
		go func(fu fleetWatcherUnit) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := act(fu.unit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && !watchers.IsNonFatal(err) {
				res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", fu.unit.Repo, err))
				return
			}
			res.Changed++
		}(fu)
	}
	wg.Wait()
	sort.Strings(res.Failures)
	return res
}

// stopFleetWatchers deactivates every installed per-repo watcher unit and
// reports, on out, both what it stopped and — crucially — anything it could
// not stop, naming the repo so the user can act on it.
func stopFleetWatchers(out io.Writer) fleetWatcherResult {
	units, warnings, err := installedFleetWatcherUnits(false /* every installed unit */)
	if err != nil {
		fmt.Fprintf(out, "  ⚠️ could not enumerate repo watchers (%v); they may still be running\n", err)
		return fleetWatcherResult{Failures: []string{err.Error()}}
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "  ⚠️ %s (its watchers may still be running)\n", w)
	}
	if len(units) == 0 {
		return fleetWatcherResult{}
	}

	loader := newFleetWatcherLoader()
	res := sweepFleetWatchers(units, loader.Unload)

	fmt.Fprintf(out, "stopped %d/%d repo watcher(s) (will not restart automatically — even across reboot — until 'grafel start')\n",
		res.Changed, res.Total)
	if len(res.Failures) > 0 {
		fmt.Fprintf(out, "  ⚠️ %d repo watcher(s) could NOT be stopped and are still running:\n", len(res.Failures))
		for _, f := range res.Failures {
			fmt.Fprintf(out, "     %s\n", f)
		}
		fmt.Fprintln(out, "     Run 'grafel status' to re-check, or stop them manually.")
	}
	return res
}

// startFleetWatchers reactivates the installed per-repo watcher units for every
// group that has watchers enabled. It is the exact inverse of the sweep
// stopFleetWatchers performs, minus the groups the user turned watchers off for.
func startFleetWatchers(out io.Writer) fleetWatcherResult {
	units, warnings, err := installedFleetWatcherUnits(true /* enabled groups only */)
	if err != nil {
		fmt.Fprintf(out, "  ⚠️ could not enumerate repo watchers (%v); they were not restarted\n", err)
		return fleetWatcherResult{Failures: []string{err.Error()}}
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "  ⚠️ %s (its watchers were not restarted)\n", w)
	}
	if len(units) == 0 {
		return fleetWatcherResult{}
	}

	loader := newFleetWatcherLoader()
	res := sweepFleetWatchers(units, loader.Load)

	fmt.Fprintf(out, "started %d/%d repo watcher(s)\n", res.Changed, res.Total)
	if len(res.Failures) > 0 {
		fmt.Fprintf(out, "  ⚠️ %d repo watcher(s) could NOT be started:\n", len(res.Failures))
		for _, f := range res.Failures {
			fmt.Fprintf(out, "     %s\n", f)
		}
	}
	return res
}

// fleetWatcherSummary is the fleet state `grafel status` reports.
type fleetWatcherSummary struct {
	Installed int
	Running   int
}

// summarizeFleetWatchers reports how many per-repo watcher units are installed
// and how many the OS says are actually running. This is what makes a stop
// verifiable: if watchers survived a stop, status says so instead of showing a
// down daemon and implying nothing is happening.
func summarizeFleetWatchers() (fleetWatcherSummary, error) {
	units, _, err := installedFleetWatcherUnits(false)
	if err != nil {
		return fleetWatcherSummary{}, err
	}
	if len(units) == 0 {
		return fleetWatcherSummary{}, nil
	}
	loader := newFleetWatcherLoader()
	sum := fleetWatcherSummary{Installed: len(units)}
	for _, fu := range units {
		st, err := loader.Status(fu.unit)
		if err != nil {
			continue
		}
		if st.Running {
			sum.Running++
		}
	}
	return sum, nil
}

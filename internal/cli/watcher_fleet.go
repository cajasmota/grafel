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
// Stop/start only flip activation.
//
// The pair is NOT symmetric, and stop says so out loud rather than implying
// otherwise. A unit whose group has features.watchers off — or an orphan unit
// no group owns — is deactivated by stop and is NOT reactivated by start; the
// only way back is `grafel install`. stopFleetWatchers names every such unit
// on stdout. (Claiming a reversibility the code does not have would repeat the
// stale comment at watcher_ctl.go's head, which asserted the daemon owned all
// watchers and is a large part of why this bug went unnoticed.)
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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// errWatchersNotStopped marks a stop that left at least one watcher running.
//
// It exists so `grafel stop` can FAIL when it did not do what it says on the
// tin. The first cut printed the warnings and returned nil, so a stop that
// deactivated 0 of 140 watchers still exited 0 and any `&&` chain or script
// read it as success — the original defect with more text in it.
//
// A sentinel (not a bare error) so runDaemonRestart can tolerate it the way it
// already tolerates errStopNotConfirmed: restart re-activates the fleet
// immediately afterward, so a watcher that resisted deactivation is not a
// reason to abort the restart.
var errWatchersNotStopped = errors.New("some repo watchers could not be stopped")

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

// fleetSweepTimeout bounds the WHOLE fleet sweep. Each individual OS call is
// separately bounded inside the platform loader, but 140 units at 8-way
// concurrency could still add up, and a `grafel stop` that hangs is a worse
// failure than the one this file fixes. Units not reached before the deadline
// are reported as not stopped — never silently skipped. A var so tests can
// shrink it.
var fleetSweepTimeout = 90 * time.Second

// fleetWatcherUnit pairs a unit with its on-disk path.
type fleetWatcherUnit struct {
	unit watchers.Unit
	path string
	// display is what to call this unit in output: the repo path when we know
	// it, else the bare label (an orphan discovered by scanning the unit dir).
	display string
	// restorable is false when `grafel start` will NOT bring this unit back —
	// its group has features.watchers off, or it is an orphan with no group at
	// all. Stop still deactivates it, but it must say so: that combination is a
	// one-way door out of which only `grafel install` leads.
	restorable bool
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
	bin, _ := os.Executable() // best effort; Label/UnitPath do not depend on it

	// Pass 1 — the registry's view: "which units SHOULD exist, and do they?"
	// This is the only pass that knows a unit's repo path and its group's
	// features.watchers flag, so it is what start is driven from.
	byPath := map[string]fleetWatcherUnit{}
	var warnings []string
	refs, err := registry.Groups()
	if err != nil {
		return nil, nil, fmt.Errorf("read group registry: %w", err)
	}
	for _, ref := range refs {
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			// The units of an unreadable group are still on disk and still
			// running. Pass 2 picks them up by label; this warning explains why
			// they show up without a repo path.
			warnings = append(warnings, fmt.Sprintf("group %s: %v", ref.Name, err))
			continue
		}
		enabled := cfg.Features.Watchers
		if onlyEnabled && !enabled {
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
			byPath[path] = fleetWatcherUnit{
				unit: u, path: path, display: r.Path, restorable: enabled,
			}
		}
	}

	// Pass 2 — the DISK's view: "what is actually installed?"
	//
	// Skipped for start (onlyEnabled), which must never activate a unit it
	// cannot attribute to a group the user has watchers enabled for.
	//
	// For stop this pass is the point. The registry cannot see a unit left by a
	// partially-failed watchers.Cleanup (best-effort: `_ = loader.Unload(u); _
	// = Remove(u)`), a unit belonging to the unreadable group warned about
	// above, a unit whose group was dropped from registry.json, or a unit whose
	// label was derived by an older slug rule. Every one of those is a live
	// `grafel watch` process. Enumerating only from the registry answers
	// "which units should exist?" — and taking that for an answer to "what is
	// running?" is the exact reasoning error that produced the original bug.
	if !onlyEnabled {
		disk, derr := watchers.InstalledUnits()
		if derr != nil {
			warnings = append(warnings, fmt.Sprintf("scan watcher unit dir: %v", derr))
		}
		for _, du := range disk {
			path, perr := watchers.UnitPath(du)
			if perr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", du.Label(), perr))
				continue
			}
			if _, seen := byPath[path]; seen {
				continue // already attributed to a group, with richer info
			}
			byPath[path] = fleetWatcherUnit{
				unit: du, path: path, display: du.Label(),
				restorable: false, // no group owns it; start cannot restore it
			}
		}
	}

	units := make([]fleetWatcherUnit, 0, len(byPath))
	for _, u := range byPath {
		units = append(units, u)
	}
	sort.Slice(units, func(i, j int) bool { return units[i].path < units[j].path })
	return units, warnings, nil
}

// sweepFleetWatchers applies act to every enumerated unit, bounded by
// fleetWatcherConcurrency, and collects failures.
// It is bounded by fleetSweepTimeout: a unit whose OS call has not returned by
// the deadline is counted as NOT changed and named in Failures. The sweep never
// waits on a straggler past the deadline — `launchctl bootout` blocks until the
// job exits and `grafel watch` drains on SIGTERM, so one wedged watcher could
// otherwise hang the whole command with no output at all.
func sweepFleetWatchers(units []fleetWatcherUnit, act func(watchers.Unit) error) fleetWatcherResult {
	res := fleetWatcherResult{Total: len(units)}
	if len(units) == 0 {
		return res
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetSweepTimeout)
	defer cancel()

	type outcome struct {
		display string
		err     error
	}
	results := make(chan outcome, len(units))
	sem := make(chan struct{}, fleetWatcherConcurrency)
	for _, fu := range units {
		go func(fu fleetWatcherUnit) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- outcome{fu.display, ctx.Err()}
				return
			}
			results <- outcome{fu.display, act(fu.unit)}
		}(fu)
	}

	// Collect exactly len(units) outcomes, or give up at the deadline and
	// report whatever has not come back. Workers still in flight are left to
	// finish and drain into the buffered channel; they are never joined, which
	// is the point — this function must always return.
	pending := map[string]bool{}
	for _, fu := range units {
		pending[fu.display] = true
	}
	for i := 0; i < len(units); i++ {
		select {
		case o := <-results:
			delete(pending, o.display)
			if o.err != nil && !watchers.IsNonFatal(o.err) {
				res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", o.display, o.err))
				continue
			}
			res.Changed++
		case <-ctx.Done():
			for d := range pending {
				res.Failures = append(res.Failures,
					fmt.Sprintf("%s: timed out after %s", d, fleetSweepTimeout))
			}
			sort.Strings(res.Failures)
			return res
		}
	}
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

	// Name the one-way doors. Stop is deliberately ungated by
	// features.watchers — a unit on disk is running whatever the config now
	// says — but start IS gated, so these specific units will not come back
	// from `grafel start`. Saying nothing would make the stop/start pair look
	// symmetric when it is not, and a comment claiming a property the code does
	// not have is exactly the failure that hid the original bug.
	var oneWay []string
	for _, fu := range units {
		if !fu.restorable {
			oneWay = append(oneWay, fu.display)
		}
	}
	if len(oneWay) > 0 {
		sort.Strings(oneWay)
		fmt.Fprintf(out, "  note: %d of these will NOT be restored by 'grafel start' "+
			"(their group has watchers disabled, or no group owns them). "+
			"Run 'grafel install' to re-enable:\n", len(oneWay))
		for _, d := range oneWay {
			fmt.Fprintf(out, "     %s\n", d)
		}
	}

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

	// Bounded parallelism, not a serial loop. `grafel status` is now the
	// surface a user checks to confirm a stop actually worked, and each
	// Status() is a process spawn — 140 of them serially would make the
	// verification command noticeably slower than the thing it verifies.
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
			st, err := loader.Status(fu.unit)
			if err != nil || !st.Running {
				return
			}
			mu.Lock()
			sum.Running++
			mu.Unlock()
		}(fu)
	}
	wg.Wait()
	return sum, nil
}

// watcher_unit_unload.go — the production half of ReaperConfig.UnloadWatcherUnit
// (#6187).
//
// The foreign-watcher sweep decides WHICH repos were left with no watcher after
// a reap (reaper.go, unit-tested against a fake hook). This file is the part
// that touches the machine: it maps a repo path back to its installed watcher
// unit and deregisters that unit from the OS scheduler.
//
// Why the reaper needs this at all: SIGTERMing the process is only half a reap.
// The launchd job stays bootstrapped, so `launchctl list` and every tool built
// on it still report a loaded watcher for a repo that has none — and since
// #6179 the watcher exits 0 on SIGTERM, so launchd deliberately never brings it
// back. Booting the unit out is what turns "silently broken" into "visibly
// absent", which is a state `grafel doctor` reports and `grafel start` /
// `grafel install` can put right.
//
// The unit FILE is deliberately left on disk. It is the repo's configuration,
// not its runtime state; deleting it would discard information the user did not
// ask us to discard and would make re-establishment harder, not easier.
package daemon

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// newWatcherUnitLoader constructs the OS scheduler loader. A package var so
// tests can observe which unit was deregistered: the real darwin loader probes
// `launchctl list` first and short-circuits when the label is not loaded (which
// it never is under a sandboxed HOME), so stubbing the command runner alone
// cannot show the label. Production never replaces it.
var newWatcherUnitLoader = watchers.NewLoader

// makeWatcherUnitUnloader returns the ReaperConfig.UnloadWatcherUnit hook.
//
// It is conservative by construction. It acts only on a repo that is REGISTERED
// (so the group — and therefore the unit label — is derived, never guessed) and
// whose unit file is actually present on disk (so it never calls the scheduler
// about a unit nothing installed). Anything else is a no-op with a debug line.
func makeWatcherUnitUnloader(logger *slog.Logger) func(repoPath string) {
	return func(repoPath string) {
		u, unitPath, ok := watcherUnitForRepo(repoPath, logger)
		if !ok {
			logger.Debug("reaper: no installed watcher unit for reaped repo — nothing to deregister",
				"repo", repoPath)
			return
		}
		// Unload, not Cleanup: deactivate the registration, keep the unit file.
		if err := newWatcherUnitLoader().Unload(u); err != nil {
			logger.Warn("reaper: could not deregister watcher unit after reaping its watcher (non-fatal)",
				"repo", repoPath, "label", u.Label(), "unit", unitPath, "err", err)
			return
		}
		logger.Info("reaper: deregistered watcher unit whose watcher was reaped as foreign (#6187) — "+
			"run `grafel install` or `grafel start` to re-establish it",
			"repo", repoPath, "label", u.Label(), "unit", unitPath)
	}
}

// watcherUnitForRepo resolves repoPath to its installed watcher unit, returning
// the unit, its on-disk path, and whether one was found.
//
// The group is what makes a label derivable, and only the registry knows it, so
// an unregistered repo yields nothing rather than a guess. A group whose config
// cannot be read is skipped: one unreadable fleet.json must not stop the others
// from being resolved.
func watcherUnitForRepo(repoPath string, logger *slog.Logger) (watchers.Unit, string, bool) {
	bin, _ := os.Executable() // best effort; Label/UnitPath do not depend on it.
	want := filepath.Clean(repoPath)

	refs, err := registry.Groups()
	if err != nil {
		logger.Warn("reaper: cannot read group registry to locate a watcher unit (non-fatal)", "err", err)
		return watchers.Unit{}, "", false
	}
	for _, ref := range refs {
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			logger.Debug("reaper: skipping unreadable group config while locating a watcher unit",
				"group", ref.Name, "err", err)
			continue
		}
		for _, r := range cfg.Repos {
			if filepath.Clean(r.Path) != want {
				continue
			}
			u := watchers.Unit{Group: ref.Name, Repo: r.Path, BinPath: bin}
			path, err := watchers.UnitPath(u)
			if err != nil {
				logger.Debug("reaper: cannot derive unit path", "group", ref.Name, "repo", r.Path, "err", err)
				continue
			}
			if _, err := os.Stat(path); err != nil {
				continue // no unit installed for this repo — nothing to deregister.
			}
			return u, path, true
		}
	}
	return watchers.Unit{}, "", false
}

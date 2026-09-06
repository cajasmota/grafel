package cli

// group_track_worktrees_6918_test.go — `--track-worktrees` enables per-worktree
// tracking WITHOUT the OS watcher-unit install transaction.
//
// The consumer this exists for runs grafel in minimal Docker containers: PID 1
// is `docker-init`, there is no systemd and no `systemctl` on PATH. The
// in-process fsnotify path behind `features.track_worktrees` works fine there;
// the watcher-unit install does not. Before #6918 the only CLI route to
// `track_worktrees` was `--watchers` (the daemon opts a group in when
// `TrackWorktrees || Watchers`, cmd/grafel/daemon.go), which drags the whole
// unit-install transaction along with it.
//
// ── Why this asserts SkipWatchers and not just the unit file ────────────────
//
// install.Apply gates its watcher branch on `!opts.SkipWatchers &&
// opts.Config.Features.Watchers` (internal/install/install.go:303) — BOTH.
// So the regression this file exists to prevent, wiring --track-worktrees into
// SkipWatchers (`SkipWatchers: !(f.watchers || f.trackWorktrees)`), writes no
// unit file either, because Features.Watchers is still false. A test that only
// looked at the emitted artefact would score that mutant ALIVE while it sat one
// unrelated edit away from re-introducing the container problem. The option
// handed to the transaction is therefore asserted directly, through the
// applyGroupConfigFn seam, AND the artefact is asserted alongside it.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// captureGroupApplyOptions swaps the applyGroupConfigFn seam for one that
// records the options `group add` derives from its flags and then delegates to
// the real implementation, so the install transaction still runs for real.
func captureGroupApplyOptions(t *testing.T, got *groupApplyOptions) {
	t.Helper()
	prev := applyGroupConfigFn
	applyGroupConfigFn = func(out io.Writer, cfg *registry.GroupConfig, ga groupApplyOptions) (*install.Result, error) {
		*got = ga
		return prev(out, cfg, ga)
	}
	t.Cleanup(func() { applyGroupConfigFn = prev })
}

// TestGroupAdd_TrackWorktreesIsIndependentOfWatchers is the four-cell table
// over the two flags. The `--track-worktrees` row is the load-bearing one.
func TestGroupAdd_TrackWorktreesIsIndependentOfWatchers(t *testing.T) {
	cases := []struct {
		name string
		// inputs
		watchers       bool
		trackWorktrees bool
		// expected persisted features
		wantWatchers       bool
		wantTrackWorktrees bool
		// expected install-transaction option
		wantSkipWatchers bool
		// expected artefact: a watcher unit file on disk
		wantUnitFile bool
	}{
		{
			name:               "track-worktrees alone installs no watcher units",
			trackWorktrees:     true,
			wantTrackWorktrees: true,
			wantWatchers:       false,
			wantSkipWatchers:   true,
			wantUnitFile:       false,
		},
		{
			name:               "watchers alone is unchanged",
			watchers:           true,
			wantWatchers:       true,
			wantTrackWorktrees: false,
			wantSkipWatchers:   false,
			wantUnitFile:       true,
		},
		{
			name:               "both compose",
			watchers:           true,
			trackWorktrees:     true,
			wantWatchers:       true,
			wantTrackWorktrees: true,
			wantSkipWatchers:   false,
			wantUnitFile:       true,
		},
		{
			name:               "neither",
			wantWatchers:       false,
			wantTrackWorktrees: false,
			wantSkipWatchers:   true,
			wantUnitFile:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The --watchers rows reach install.Apply's loader activation, a
			// MUTATING service-manager verb. Those are stubbed so no test can
			// drive the developer's real launchd/systemd (see
			// internal/install/watchers/test_isolation_guard.go); the unit-file
			// write they assert on stays entirely real.
			//
			// The rows WITHOUT --watchers are deliberately left unstubbed. The
			// claim being made is "--track-worktrees shells out to no service
			// manager", and the guard is what enforces it: any mutating verb
			// reaching a real exec under `go test` PANICS. Stubbing those rows
			// would replace that proof with a no-op and leave the claim
			// untested.
			if tc.watchers {
				defer watchers.StubServiceCallsForTest()()
			}

			home := withSandboxHome(t)
			repo := filepath.Join(home, "repos", "alpha")
			makeRepo(t, repo)

			var opts groupApplyOptions
			captureGroupApplyOptions(t, &opts)

			cmd := &cobra.Command{}
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			err := runGroupAddImpl(cmd, "demo", groupAddFlags{
				repoArgs:       []string{repo},
				watchers:       tc.watchers,
				trackWorktrees: tc.trackWorktrees,
				gitHooks:       false,
				rules:          false,
				mcp:            false,
				runInst:        true,
				doIndex:        false,
				jsonOut:        true,
			}, "")
			if err != nil {
				t.Fatalf("group add: %v\n%s", err, out.String())
			}

			// The persisted group config, read back from disk rather than from
			// the in-memory struct the command happened to build.
			path, err := registry.ConfigPathFor("demo")
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := registry.LoadGroupConfig(path)
			if err != nil {
				t.Fatalf("LoadGroupConfig: %v", err)
			}
			if cfg.Features.TrackWorktrees != tc.wantTrackWorktrees {
				t.Errorf("features.track_worktrees = %v, want %v",
					cfg.Features.TrackWorktrees, tc.wantTrackWorktrees)
			}
			if cfg.Features.Watchers != tc.wantWatchers {
				t.Errorf("features.watchers = %v, want %v",
					cfg.Features.Watchers, tc.wantWatchers)
			}

			// The install transaction's watcher branch, as handed to it.
			if opts.SkipWatchers != tc.wantSkipWatchers {
				t.Errorf("install options SkipWatchers = %v, want %v "+
					"(--track-worktrees must never move this)",
					opts.SkipWatchers, tc.wantSkipWatchers)
			}

			// And the artefact that option decides.
			unit := unitPathForRepo(t, "demo", repo)
			_, statErr := os.Stat(unit)
			gotUnitFile := statErr == nil
			if gotUnitFile != tc.wantUnitFile {
				t.Errorf("watcher unit file present = %v (%s), want %v",
					gotUnitFile, unit, tc.wantUnitFile)
			}
		})
	}
}

// TestGroupAddCmd_TrackWorktreesFlagDefaultsOff pins the flag's registration
// and its default. The default is load-bearing: track_worktrees is documented
// "opt-in to preserve existing behaviour" (internal/registry/registry.go), and
// a default of true would turn every `group add` into a worktree-polling group.
func TestGroupAddCmd_TrackWorktreesFlagDefaultsOff(t *testing.T) {
	f := newGroupAddCmd().Flags().Lookup("track-worktrees")
	if f == nil {
		t.Fatal("group add has no --track-worktrees flag")
	}
	if f.Value.Type() != "bool" {
		t.Errorf("--track-worktrees is %s, want bool", f.Value.Type())
	}
	if f.DefValue != "false" {
		t.Errorf("--track-worktrees defaults to %q, want \"false\"", f.DefValue)
	}
	// It must not be quietly aliased onto --watchers.
	if w := newGroupAddCmd().Flags().Lookup("watchers"); w == nil || w.DefValue != "false" {
		t.Errorf("--watchers default changed: %+v", w)
	}
}

// TestGroupAddCmd_TrackWorktreesFlagIsBoundToTheField pins the wiring from the
// parsed cobra flag through to the persisted feature — the RunE closure, which
// the table test above bypasses by constructing groupAddFlags directly.
func TestGroupAddCmd_TrackWorktreesFlagIsBoundToTheField(t *testing.T) {
	// Not stubbed, on purpose: see the table test above. Under `go test` the
	// watchers package refuses (panics on) any mutating service-manager call
	// that reaches a real exec, so this run passing is positive evidence that
	// --track-worktrees alone reached none.
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repo)

	var opts groupApplyOptions
	captureGroupApplyOptions(t, &opts)

	cmd := newGroupAddCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"demo", "--repo", repo, "--track-worktrees",
		"--git-hooks=false", "--rules=false", "--mcp=false", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}

	path, err := registry.ConfigPathFor("demo")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := registry.LoadGroupConfig(path)
	if err != nil {
		t.Fatalf("LoadGroupConfig: %v", err)
	}
	if !cfg.Features.TrackWorktrees {
		t.Error("--track-worktrees did not set features.track_worktrees")
	}
	if cfg.Features.Watchers {
		t.Error("--track-worktrees set features.watchers")
	}
	if !opts.SkipWatchers {
		t.Error("--track-worktrees cleared SkipWatchers on the install transaction")
	}
}

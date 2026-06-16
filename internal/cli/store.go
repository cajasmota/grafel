// store.go — `grafel store` operator commands (issue #5263).
//
// `grafel store gc` attributes every top-level store root under the grafel
// store and reaps the ORPHANs — roots whose source repo/worktree no longer
// exists AND that map to no live registered group. It mirrors the daemon's
// reaper safety model exactly (deadref.go / orphanroot.go):
//
//   - A root whose source path still exists is NEVER reaped.
//   - A root mapping to a live registered group / primary is NEVER reaped.
//   - A root within the grace window is NEVER reaped.
//   - A root whose source path is UNDETERMINABLE (maps to no known repo or
//     worktree) is KEPT — fail-closed. The store-root hash is one-way, so the
//     only authoritative attribution is forward (known path → expected root).
//
// Default (and --dry-run) only PRINTS the attribution + would-reclaim bytes.
// --prune actually removes the orphan roots. The command operates directly on
// the store directory with the same liveness checks whether or not the daemon
// is running; it never auto-runs prune.
package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/registry"
)

func newStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Inspect and garbage-collect the grafel graph store",
		Long: `Operator commands for the grafel graph store (~/.grafel/store).

Subcommands:
  gc   Attribute every top-level store root and (with --prune) reap orphans —
       roots whose source repo/worktree is gone and that map to no live group.`,
	}
	cmd.AddCommand(newStoreGCCmd())
	return cmd
}

func newStoreGCCmd() *cobra.Command {
	var prune bool
	var dryRun bool
	var includeUndeterminable bool
	var olderThan string
	var yes bool
	cmd := &cobra.Command{
		Use:   "gc [--prune]",
		Short: "Reap orphan top-level store roots (default: dry-run)",
		Long: `Enumerate every top-level store root and print its attribution:
source path, whether that path still exists, whether it maps to a live
registered group, ref count, on-disk size, age of newest graph artifact, and a
verdict (KEEP / ORPHAN-would-prune).

An ORPHAN is a root whose source path is GONE and which maps to no live
group/primary and is outside the 24h grace window. A root now records its
canonical source path in a root.json manifest (#5267), so even a root whose
repo was removed from the registry / deleted from disk is attributable and
reapable. A LEGACY root with no manifest that maps to NO known repo/worktree is
reported UNDETERMINABLE and KEPT (fail-closed).

By default this is a DRY RUN — nothing is removed. Pass --prune to actually
reap the ORPHAN roots and reclaim their bytes.

Operator opt-in (#5268), for legacy stores with undeterminable roots:
  --include-undeterminable  also consider undeterminable (legacy / unmapped)
                            roots. REQUIRES --older-than. Reaping a root is
                            recoverable — it re-indexes automatically if needed.
  --older-than <dur>        only reap an undeterminable root whose newest graph
                            artifact is older than this (e.g. 168h, 7d, 30d).
  --yes                     REQUIRED to actually prune in --include-undeterminable
                            mode; without it the run stays a dry-run even with
                            --prune (guard against accidental aggressive reap).

The periodic in-daemon reaper NEVER uses the opt-in path — it stays fail-closed.
Safe to run whether or not the daemon is running.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var reapOlderThan time.Duration
			if olderThan != "" {
				d, perr := parseDurationWithDays(olderThan)
				if perr != nil {
					return fmt.Errorf("invalid --older-than %q: %w", olderThan, perr)
				}
				reapOlderThan = d
			}
			if includeUndeterminable && reapOlderThan <= 0 {
				return fmt.Errorf("--include-undeterminable requires --older-than <dur> (e.g. --older-than 7d) so undeterminable roots are reaped only beyond an explicit age bound")
			}
			// --yes is the prune authorisation for opt-in mode. Without it the
			// run is forced to dry-run even if --prune was passed.
			effectivePrune := prune
			if includeUndeterminable && !yes {
				effectivePrune = false
			}
			return runStoreGC(cmd.OutOrStdout(), storeGCOpts{
				prune:                 effectivePrune,
				includeUndeterminable: includeUndeterminable,
				reapOlderThan:         reapOlderThan,
				yesWithheld:           includeUndeterminable && !yes && prune,
			}, cliKnownSourcePaths)
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false,
		"actually remove orphan roots (default: dry-run, nothing removed)")
	// --dry-run is accepted for symmetry with `grafel cleanup`; it is the
	// default and simply the negation of --prune. Explicit --dry-run wins.
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"list orphans without removing them (default behaviour)")
	cmd.Flags().BoolVar(&includeUndeterminable, "include-undeterminable", false,
		"also consider undeterminable (legacy/unmapped) roots; requires --older-than and --yes to prune")
	cmd.Flags().StringVar(&olderThan, "older-than", "",
		"in --include-undeterminable mode, only reap roots untouched longer than this (e.g. 168h, 7d, 30d)")
	cmd.Flags().BoolVar(&yes, "yes", false,
		"confirm the aggressive opt-in reap; REQUIRED to prune when --include-undeterminable is set")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if dryRun {
			prune = false
		}
		return nil
	}
	return cmd
}

// parseDurationWithDays parses a Go duration, additionally accepting a trailing
// "d" day suffix (e.g. "7d" → 168h, "30d" → 720h) which time.ParseDuration does
// not support. A bare "d" value is multiplied into hours before delegating.
func parseDurationWithDays(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		// Guard against "d" colliding with a valid unit-less suffix: only treat
		// it as days when the remainder is a plain number.
		if n, err := strconv.ParseFloat(strings.TrimSpace(rest), 64); err == nil {
			return time.Duration(n * float64(24*time.Hour)), nil
		}
	}
	return time.ParseDuration(s)
}

// storeGCOpts bundles the resolved `store gc` flags for runStoreGC.
type storeGCOpts struct {
	// prune is the EFFECTIVE prune decision (already gated by --yes in opt-in
	// mode): true → actually remove orphans.
	prune bool
	// includeUndeterminable enables the operator opt-in reap of undeterminable
	// (legacy/unmapped) roots, bounded by reapOlderThan.
	includeUndeterminable bool
	// reapOlderThan is the explicit age bound for the opt-in reap (>0 required
	// when includeUndeterminable is set).
	reapOlderThan time.Duration
	// yesWithheld is true when --prune --include-undeterminable was requested
	// WITHOUT --yes, so we forced dry-run and want to tell the operator why.
	yesWithheld bool
}

func runStoreGC(w io.Writer, opts storeGCOpts, knownPaths func() []string) error {
	sweeper := daemon.NewOrphanRootSweeper(daemon.OrphanRootConfig{
		KnownSourcePaths:        knownPaths,
		AllowUndeterminableReap: opts.includeUndeterminable,
		ReapOlderThan:           opts.reapOlderThan,
	})
	prune := opts.prune

	verdicts := sweeper.Attribute()
	// Stable, browsable order: orphans first, then by size descending.
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].IsOrphan() != verdicts[j].IsOrphan() {
			return verdicts[i].IsOrphan()
		}
		return verdicts[i].SizeBytes > verdicts[j].SizeBytes
	})

	var totalBytes, orphanBytes int64
	var orphanCount int
	for _, v := range verdicts {
		totalBytes += v.SizeBytes
		if v.IsOrphan() {
			orphanCount++
			orphanBytes += v.SizeBytes
		}
		printVerdict(w, v)
	}

	fmt.Fprintf(w, "\n%d roots, %s total; %d orphan(s), %s would reclaim\n",
		len(verdicts), hbytes(totalBytes), orphanCount, hbytes(orphanBytes))

	if opts.includeUndeterminable {
		fmt.Fprintf(w, "(opt-in: undeterminable roots untouched > %s are reapable; reaped roots re-index automatically if needed later.)\n",
			humanAge(opts.reapOlderThan))
	}

	if !prune {
		if orphanCount > 0 {
			if opts.yesWithheld {
				fmt.Fprintln(w, "\n(dry-run) --include-undeterminable requires --yes to prune; re-run with --prune --yes to reap the ORPHAN roots above.")
			} else if opts.includeUndeterminable {
				fmt.Fprintln(w, "\n(dry-run) Run 'grafel store gc --include-undeterminable --older-than <dur> --prune --yes' to reap the ORPHAN roots above.")
			} else {
				fmt.Fprintln(w, "\n(dry-run) Run 'grafel store gc --prune' to reap the ORPHAN roots above.")
			}
		}
		return nil
	}

	if orphanCount == 0 {
		fmt.Fprintln(w, "\nNothing to prune.")
		return nil
	}

	res := sweeper.Sweep()
	fmt.Fprintf(w, "\n✓ Pruned %d orphan root(s), reclaimed %s.\n",
		res.RootsReaped, hbytes(res.FreedBytes))
	if res.RootsReaped < orphanCount {
		fmt.Fprintf(w, "  (%d candidate(s) kept — removal failed or became live mid-sweep)\n",
			orphanCount-res.RootsReaped)
	}
	if opts.includeUndeterminable {
		fmt.Fprintln(w, "  Reaped roots re-index automatically the next time their source path is registered/indexed.")
	}
	return nil
}

func printVerdict(w io.Writer, v daemon.OrphanRootVerdict) {
	mark := "KEEP  "
	if v.IsOrphan() {
		mark = "ORPHAN"
	}
	src := v.SourcePath
	if src == "" {
		src = "<undeterminable>"
	}
	exists := "gone"
	if v.PathExists {
		exists = "exists"
	}
	fmt.Fprintf(w, "%s  %s\n", mark, filepath.Base(v.Root))
	fmt.Fprintf(w, "        src=%s (%s)  refs=%d  size=%s  age=%s\n",
		src, exists, v.RefCount, hbytes(v.SizeBytes), humanAge(v.AgeOfNewest))
	fmt.Fprintf(w, "        %s\n", v.Reason)
}

// cliKnownSourcePaths is the operator command's KnownSourcePaths provider. It
// works WITHOUT a running daemon: it reads the registry directly for every
// registered-group repo and enumerates each repo's git worktrees (so a root for
// a still-checked-out worktree is correctly attributed to a live path). Paths
// whose directory is gone are still included so their root can be attributed
// and reaped; a root attributable to NO path here is left undeterminable
// (fail-closed KEEP) by the sweeper.
func cliKnownSourcePaths() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	}

	groups, err := registry.Groups()
	if err != nil {
		return out
	}
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			add(r.Path)
			// Enumerate the repo's worktrees so worktree roots map to a live
			// path. Best-effort: a non-git / gone repo simply yields nothing.
			for _, wt := range gitWorktreePaths(r.Path) {
				add(wt)
			}
		}
	}
	return out
}

// gitWorktreePaths returns the absolute paths of every worktree linked to
// repoPath (including the main worktree) via `git worktree list --porcelain`.
// Returns nil for a non-git / unreadable repo.
func gitWorktreePaths(repoPath string) []string {
	out := gitmeta.RunGit(repoPath, "worktree", "list", "--porcelain")
	if out == "" {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if p, ok := strings.CutPrefix(line, "worktree "); ok && p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// hbytes adapts the package humanBytes (uint64) to the int64 sizes the store
// sweeper reports, clamping negatives to 0.
func hbytes(b int64) string {
	if b < 0 {
		b = 0
	}
	return humanBytes(uint64(b))
}

// humanAge renders a duration as a compact age (e.g. "3d", "5h", "—" for zero).
func humanAge(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

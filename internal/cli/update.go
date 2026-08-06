package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/hooks"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/registry"
)

func newUpdateCmd() *cobra.Command {
	var (
		// Legacy flags (preserved for backward compat).
		refreshLite bool

		// New self-update flags (#2213).
		tag string
		pre bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update grafel to the latest release (or a pinned version)",
		Long: `Update downloads the latest grafel release from GitHub, atomically
replaces the current binary, and re-runs the install transaction
(skills, MCP, daemon restart).

On success the previous binary is removed. On failure the previous
binary is restored automatically (rollback).

Update never modifies the git repository you happen to be standing in:
the .gitignore entry and the git hooks belong to 'grafel install', which
you run inside a repo on purpose.

  grafel update                # latest stable release
  grafel update --pre          # latest pre-release
  grafel update --tag v1.2.3   # pin to a specific version

The update is idempotent: if the binary is already at the target
version the command exits 0 without downloading anything.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// Legacy behaviour: when neither --tag nor --pre is set AND no
			// GitHub connectivity is needed, fall through to the old group-
			// refresh path.  The presence of --tag or --pre signals the new
			// self-update path.
			if tag != "" || pre {
				return runSelfUpdate(out, install.UpdateOptions{
					Tag: tag,
					Pre: pre,
				})
			}

			if refreshLite {
				fmt.Fprintln(out, "refreshing rules-lite (no-op in current build)")
			}

			// ── legacy group-refresh path ─────────────────────────────────
			bin, _ := os.Executable()
			groups, err := registry.Groups()
			if err != nil {
				return err
			}
			for _, g := range groups {
				cfg, err := registry.LoadGroupConfig(g.ConfigPath)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", g.Name, err)
					continue
				}
				for _, r := range cfg.Repos {
					if cfg.Features.GitHooks {
						if err := hooks.Install(r.Path, bin); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "hooks %s: %v\n", r.Slug, err)
						}
					}
				}
				// Re-run install to refresh MCP entries.
				//
				// SkipWatchers (#6179 F1): this loop runs BEFORE runSelfUpdate
				// swaps the binary, so `bin` here is the OLD grafel and Apply
				// would render the OLD unit template — writing and re-registering
				// every watcher with exactly the settings we are trying to
				// replace, and then having the post-swap reconcile rewrite and
				// re-register all of them again. At 140 repos that is 280
				// launchctl bootout+bootstrap cycles — two rounds of the macOS
				// Background Items notification burst — to reach a state one
				// round reaches. Watcher units are owned by the post-swap
				// reconcile in runSelfUpdate, which renders the NEW template and
				// touches only the units whose content actually changed.
				_, _ = install.Apply(install.Options{
					Group:        g.Name,
					Config:       cfg,
					BinPath:      bin,
					SkipWatchers: true,
				})
			}
			// MCP entries should always reflect the live binary path.
			regPath, _ := registry.RegistryPath()
			_, _ = mcpreg.Register(mcpreg.ClaudeCode, bin, regPath)
			_, _ = mcpreg.Register(mcpreg.Windsurf, bin, regPath)

			// Also run the new self-update with latest stable tag (downloads).
			return runSelfUpdate(out, install.UpdateOptions{})
		},
	}
	cmd.Flags().BoolVar(&refreshLite, "refresh-rules-lite", false, "refresh the lite rule-pack (no-op)")
	cmd.Flags().StringVar(&tag, "tag", "",
		"pin update to a specific release tag (e.g. v1.2.3)")
	cmd.Flags().BoolVar(&pre, "pre", false,
		"allow pre-release tags when resolving 'latest'")
	return cmd
}

// refreshWatcherUnitsWithNewBinary re-invokes the just-installed binary as
// `grafel install --refresh-state`, whose handler reconciles stale watcher unit
// files (#6179 F1).
//
// Why a subprocess: after RunUpdate's atomic rename the file at os.Executable()
// is the NEW grafel, but this process is still running the OLD code, including
// the OLD unit template. Rendering the fixed template requires executing the
// new binary. `install --refresh-state` is reused rather than a new subcommand
// because it is the same narrow, non-mutating entrypoint the curl installer
// already calls for exactly this "make on-disk state agree with the binary I
// just placed" purpose.
//
// Best-effort and silent on success: an update must not fail because a watcher
// plist could not be rewritten. A package var so tests can assert the wiring
// without spawning anything.
// watcherRefreshTimeout bounds how long refreshWatcherUnitsWithNewBinary will
// wait for the child before giving up (#6184).
//
// At 140 repos the child makes hundreds of serial launchctl calls
// (loader_darwin.go:64-100 retries bootstrap up to 3x on exit-5 with a 200ms
// backoff). With a wedged launchd — a documented failure mode — the child
// itself can hang indefinitely. 3 minutes is generous for 140 units' worth of
// launchctl round-trips under normal conditions while still bounding the
// worst case instead of inheriting it.
var watcherRefreshTimeout = 3 * time.Minute

// watcherRefreshWaitDelay caps how long CombinedOutput will wait, AFTER the
// context deadline fires and the direct child is signalled, before os/exec
// force-closes the child's I/O pipes and returns.
//
// #6184 F1 (found on review): exec.CommandContext only SIGKILLs the DIRECT
// child. CombinedOutput backs stdout/stderr with an os.Pipe, and Wait()
// blocks on that pipe's read side until every WRITER closes it — including
// any grandchild that inherited the fd. install --refresh-state's own child
// is exactly launchctl, which is what #6184 says can be wedged: killing the
// direct child leaves a wedged launchctl grandchild holding the pipe open,
// so watcherRefreshTimeout alone does not bound wall-clock time at all.
// Measured: a 1s deadline against a process with a surviving grandchild
// returned in 20.55s, not ~1s.
//
// This is the same failure mode fixed for git children in
// internal/gitmeta/gitmeta.go (applyWaitDelay / waitDelayGrace, #5286);
// WaitDelay (Go 1.20+) is the precedented fix, reused here rather than
// invented fresh. 3s matches gitmeta's value: enough for a clean process to
// flush and exit, short enough not to reintroduce the original hang.
const watcherRefreshWaitDelay = 3 * time.Second

// runWatcherRefreshCmd runs the `install --refresh-state` child and returns
// its combined output. A package var so tests can substitute a child that
// blocks past the deadline without needing a real wedged launchd.
var runWatcherRefreshCmd = func(ctx context.Context, bin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, "install", "--refresh-state")
	cmd.WaitDelay = watcherRefreshWaitDelay
	// GRAFEL_SKIP_QUICK_DOCTOR: the child would otherwise run the quick-doctor
	// preflight against a daemon that RunCopy may still be restarting, and
	// print a spurious drift warning in the middle of update's own output.
	// refreshStateQuietEnv drops the child's install-state bookkeeping lines so
	// only the watcher summary is forwarded.
	cmd.Env = append(os.Environ(),
		"GRAFEL_SKIP_QUICK_DOCTOR=1",
		refreshStateQuietEnv+"=1")
	return cmd.CombinedOutput()
}

var refreshWatcherUnitsWithNewBinary = func(out io.Writer) {
	bin, err := os.Executable()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), watcherRefreshTimeout)
	defer cancel()
	b, err := runWatcherRefreshCmd(ctx, bin)
	if ctx.Err() == context.DeadlineExceeded {
		// #6184: CombinedOutput used to buffer until exit with no deadline at
		// all, so a wedged launchd meant `grafel update` hung forever with no
		// indication of what it was doing. Report the timeout explicitly
		// instead of inheriting that silence.
		reportWatcherRefresh(out, b, fmt.Errorf(
			"timed out after %s waiting for watcher unit refresh (launchd may be wedged); "+
				"the update itself already completed, only watcher-unit reconcile was skipped",
			watcherRefreshTimeout))
		return
	}
	reportWatcherRefresh(out, b, err)
}

// reportWatcherRefresh forwards the watcher-reconcile child's output.
//
// #6179 F1-b: this previously printed only when the child FAILED, so on the
// success path — the normal path, and the one that produces the burst — the
// summary was swallowed. On a machine where all 140 units are stale the
// reconcile re-registers all of them, and loader_darwin.go retries bootstrap up
// to 3x serially, so the user can see hundreds of launchctl invocations and a
// wave of macOS Background Items notifications immediately after upgrading.
// With no output the only available reading is "the fix made it worse".
//
// Silence is preserved when the child had nothing to say, which is the steady
// state once a machine is current.
func reportWatcherRefresh(out io.Writer, combined []byte, err error) {
	text := strings.TrimSpace(string(combined))
	if err != nil {
		fmt.Fprintf(out, "  watchers: could not refresh watcher units: %v\n", err)
		if text != "" {
			fmt.Fprintf(out, "            %s\n", text)
		}
		return
	}
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(out, "  %s\n", strings.TrimSpace(line))
	}
}

// runSelfUpdate executes the new atomic self-update path (#2213) and prints a
// summary to out.
func runSelfUpdate(out io.Writer, opts install.UpdateOptions) error {
	opts.SkipDaemonRestart = false // let install handle it

	result, err := install.RunUpdate(opts)
	if err != nil {
		fmt.Fprintf(out, "✗ update failed: %v\n", err)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "The previous binary has been restored.")
		return err
	}

	// ── watcher-unit reconcile (#6179 F1) ──────────────────────────────────
	// The binary at this path has just been replaced, so THIS process is still
	// the old code and cannot render the new unit template. The new binary has
	// to do it, which is why this re-invokes it rather than calling
	// install.ReconcileWatcherUnits in-process.
	//
	// It runs on the skipped path too: "already at the latest version" only
	// says the binary matches, not that the units on disk do — a machine that
	// updated before this fix existed is exactly that case, and it is the one
	// the #6179 reporter is in.
	refreshWatcherUnitsWithNewBinary(out)

	if result.Skipped {
		fmt.Fprintf(out, "✓ grafel is already at the latest version (%s)\n", result.Tag)
		return nil
	}

	fmt.Fprintf(out, "✓ grafel updated to %s\n", result.Tag)
	if result.PreviousVersion != "" && len(result.PreviousVersion) >= 16 {
		fmt.Fprintf(out, "  previous: %s...\n", result.PreviousVersion[:16])
	}
	if result.NewVersion != "" && len(result.NewVersion) >= 16 {
		fmt.Fprintf(out, "  new:      %s...\n", result.NewVersion[:16])
	}
	if result.InstallResult != nil {
		if len(result.InstallResult.SkillsInstalled) > 0 {
			fmt.Fprintf(out, "  skills:   %d refreshed\n", len(result.InstallResult.SkillsInstalled))
		}
		if len(result.InstallResult.MCPPaths) > 0 {
			fmt.Fprintf(out, "  MCP:      refreshed in %d config file(s)\n", len(result.InstallResult.MCPPaths))
		}
		if result.InstallResult.DaemonVersion != "" {
			fmt.Fprintf(out, "  daemon:   %s\n", result.InstallResult.DaemonVersion)
		}
	}
	// #5907 FIX4: surface the auto-reindex-on-upgrade the engine has already
	// (loop-guarded) enqueued, so it reads as "reindexing after upgrade"
	// rather than a silent multi-minute stall right after `grafel update`
	// returns. Report-only — nothing here triggers or duplicates the reindex.
	if result.ReposNeedingReindex > 0 {
		fmt.Fprintf(out, "  reindex:  %d repo(s) reindexing after upgrade\n", result.ReposNeedingReindex)
	}
	return nil
}

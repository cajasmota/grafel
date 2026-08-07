package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/mode"
	"github.com/cajasmota/grafel/internal/daemon/service"
	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/skilllink"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// registerMCPInClaudeConfigs registers the grafel MCP entry in all detected
// Claude Code config directories. It's extracted into a separate function so it
// can be tested independently of service.Install, which requires OS permissions.
//
// binPath is the full path to the grafel binary.
// claudeConfigDirs, when non-empty, overrides auto-detection of ~/.claude.json dirs.
// Returns a list of successfully registered paths and prints status to out.
func registerMCPInClaudeConfigs(out io.Writer, binPath string, claudeConfigDirs []string) []string {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(claudeConfigDirs)
	registered := []string{}
	for _, cfgPath := range claudeDirs {
		if _, err := mcpreg.RegisterPath(cfgPath, binPath); err != nil {
			fmt.Fprintf(out, "  ⚠ MCP register %s: %v\n", cfgPath, err)
		} else {
			registered = append(registered, cfgPath)
		}
	}
	if len(registered) > 0 {
		fmt.Fprintf(out, "  MCP registered in:\n")
		for _, p := range registered {
			fmt.Fprintf(out, "    %s\n", p)
		}
		fmt.Fprintf(out, "  Restart Claude Code to load the grafel MCP tools.\n")
	}
	return registered
}

// installSkillsInClaudeConfigs symlinks the 15 grafel skills into every
// detected Claude Code config directory. It's extracted into a separate
// function so it can be tested independently of service.Install.
//
// binPath is the full path to the grafel binary (used to infer skills location).
// skillsSourceDir is an explicit override for the skills directory (from --skills-source-dir flag).
// claudeConfigDirs, when non-empty, overrides auto-detection of ~/.claude.json dirs.
// Returns a list of successfully installed paths and prints status to out.
func installSkillsInClaudeConfigs(out io.Writer, binPath, skillsSourceDir string, claudeConfigDirs []string) []string {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(claudeConfigDirs)
	return skilllink.InstallSkillsInClaudeConfigs(out, binPath, skillsSourceDir, claudeDirs)
}

// newInstallCmd returns the `grafel install` subcommand.
//
// Per ADR-0017 Phase C the old "apply a group config" semantic is
// REMOVED. `grafel install` is now the canonical one-liner that
// registers the daemon as a user-level OS service (launchd on macOS,
// systemd on Linux) and starts it.
//
// The --foreground flag skips service registration and just starts the
// daemon in the foreground — useful when launchd/systemd isn't
// cooperating and you need debug output directly in the terminal.
//
// The --copy flag (issue #2210) runs the full atomic COPY-mode install
// transaction: skill copy, MCP registration, daemon restart, .gitignore
// update, and install.json state persistence. This is the new default
// per epic #2197; use --copy=false to revert to the legacy symlink path.
//
// The --dev flag (issue #2212) runs the DEV-mode install transaction:
// identical to --copy but symlinks skills from the repo working tree
// instead of copying them, so edits are instantly visible to Claude Code.
func newInstallCmd() *cobra.Command {
	var foreground bool
	var claudeConfigDirs []string
	var skillsSourceDir string
	var skipSkillLink bool
	var installMode string
	var copyMode bool
	var devMode bool
	var force bool
	var noHooks bool
	var toolsCSV string
	var noWizard bool
	var assumeYes bool
	var refreshState bool
	var registerMCPOnly bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register grafel daemon as a system service and start it",
		Long: `Install registers the grafel daemon as a user-level OS service
and starts it immediately.

On macOS: writes ~/Library/LaunchAgents/com.grafel.daemon.plist and
calls 'launchctl bootstrap'. The daemon auto-starts at every login.

On Linux: writes ~/.config/systemd/user/grafel-daemon.service and
calls 'systemctl --user enable --now'.

No sudo or root is required.

Re-running install when the service is already active prints the current
status and exits successfully (idempotent).

Use --foreground to skip service registration and run the daemon directly
in this terminal — useful for debugging launchd/systemd issues.

Use --mode to select the operational preset (background, workstation, readonly).
The default is background. See 'grafel mode --help' for details.

Use --copy (default: true) to run the full atomic COPY-mode install
transaction (issue #2210): copies skills into ~/.claude/skills/, registers
the MCP server, restarts the daemon, updates .gitignore, and writes
~/.grafel/install.json with per-file SHA checksums. The second run is
a fast no-op (idempotent). Use --force to bypass the partial-install guard.

Install also copies or symlinks the grafel skills into every detected
Claude Code config directory's skills/ subdirectory.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// ── --refresh-state: record the on-disk binary, nothing else ───
			// Handled FIRST, before the tool-selection wizard and before any
			// of the install transaction: this path exists precisely so the
			// curl installer can make ~/.grafel/install.json agree with the
			// binary it just placed WITHOUT restarting the daemon, rewriting
			// .claude.json, appending to the caller's .gitignore, installing
			// git hooks in the caller's repo, or blocking on a TTY prompt.
			// See internal/install/refreshstate.go for the full argument.
			if refreshState {
				// Silently ignoring the rest would be its own trap: every
				// other install flag asks for work --refresh-state explicitly
				// does not do, so a user combining them would get a state
				// rewrite and believe they got an install.
				if conflicts := conflictingRefreshStateFlags(cmd); len(conflicts) > 0 {
					return fmt.Errorf("--refresh-state only re-records this binary in install.json; it cannot be combined with %s "+
						"(run 'grafel install' on its own for a full install)", strings.Join(conflicts, ", "))
				}
				return runRefreshState(out)
			}

			// ── --register-mcp: write the MCP entry, nothing else (#6169) ──
			// Handled in the same position and for the same reason as
			// --refresh-state, one flag above: AHEAD of resolveToolSelection
			// (whose TTY branch opens an interactive wizard that would block a
			// `curl … | bash` install mid-run) and ahead of RunCopy (which
			// restarts the daemon, appends /.grafel/ to the caller's
			// .gitignore, and writes four git hooks into a repository the
			// installer was never told about).
			//
			// This is the step install.sh was missing entirely: it placed the
			// binary and never registered the MCP server, so a first-ever curl
			// install produced a grafel that Claude Code could not see at all.
			// See internal/install/registermcp.go for the full argument.
			if registerMCPOnly {
				if conflicts := conflictingRegisterMCPFlags(cmd); len(conflicts) > 0 {
					return fmt.Errorf("--register-mcp only writes the grafel MCP entry into the detected host configs; it cannot be combined with %s "+
						"(run 'grafel install' on its own for a full install)", strings.Join(conflicts, ", "))
				}
				return runRegisterMCP(out, claudeConfigDirs)
			}

			// ── per-tool selection (#5256) ─────────────────────────────────
			// Resolve the desired tool set and persist it to every registered
			// group's GroupConfig.Tools. Precedence:
			//   1. --tools a,b,c   → explicit, validated, NON-interactive.
			//   2. interactive wizard → only when stdin is a TTY AND neither
			//      --tools nor --yes/--no-wizard was given.
			//   3. otherwise (no flag, no TTY, or --yes/--no-wizard) → leave
			//      the existing selection untouched, i.e. today's behaviour
			//      (EnabledTools falls back to all tools). CI is never blocked.
			if sel, ok, err := resolveToolSelection(cmd, out, toolsCSV, noWizard, assumeYes); err != nil {
				return err
			} else if ok {
				if err := persistToolSelection(out, sel); err != nil {
					return err
				}
			}

			if foreground {
				// --foreground: skip service registration, just run the daemon
				// in this process. Useful when launchd/systemd is misbehaving.
				fmt.Fprintln(out, "starting grafel daemon in foreground (Ctrl-C to stop)…")
				if activeHooks.RunDaemon == nil {
					return fmt.Errorf("daemon entrypoint not wired")
				}
				return activeHooks.RunDaemon(nil)
			}

			bin, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve binary path: %w", err)
			}

			// ── DEV mode path (issue #2212) ────────────────────────────────────
			// When --dev is set, run the DEV-mode install: symlinks skills from
			// the repo working tree so edits are instantly visible.  --dev takes
			// precedence over --copy when both are specified.
			if devMode {
				return runInstallDev(out, install.DevOptions{
					BinPath:          bin,
					SkillsSourceDir:  skillsSourceDir,
					ClaudeConfigDirs: claudeConfigDirs,
					Force:            force,
					NoHooks:          noHooks,
				})
			}

			// ── COPY mode path (issue #2210, epic #2197) ──────────────────────
			// When --copy is set (default: true), run the full atomic COPY-mode
			// install transaction instead of the legacy symlink path. The COPY
			// path handles skill copying, MCP, daemon restart, .gitignore, and
			// writes ~/.grafel/install.json. OS service registration is also
			// performed (via service.Install inside RunCopy's step 4).
			if copyMode {
				return runInstallCopy(out, install.CopyOptions{
					// #6162: `grafel install` is the ONE entrypoint that may
					// touch the repo it is run in (.gitignore + the four git
					// hooks) — running it here is the user asking for exactly
					// that. `grafel update` passes IntentUpgrade and gets
					// neither. RunCopy rejects an unset Intent, so this is
					// stated at the construction site rather than patched in
					// downstream where a future second caller would miss it.
					Intent:           install.IntentInstall,
					BinPath:          bin,
					SkillsSourceDir:  skillsSourceDir,
					ClaudeConfigDirs: claudeConfigDirs,
					Force:            force,
					NoHooks:          noHooks,
				})
			}

			// ── legacy path (preserved for backward compat; use --copy=false) ─

			layout, err := daemon.DefaultLayout()
			if err != nil {
				return fmt.Errorf("resolve daemon layout: %w", err)
			}

			// Persist the selected mode so the daemon reads it on every boot.
			// Default is background (low-footprint for open-source installs).
			selectedMode := mode.Background
			if installMode != "" {
				parsed, merr := mode.Parse(installMode)
				if merr != nil {
					return merr
				}
				selectedMode = parsed
			}
			cfgPath := mode.DefaultConfigPath(layout.Root)
			existing, _ := mode.LoadConfig(cfgPath) // best-effort; ignore missing-file error
			existing.Mode = selectedMode
			if serr := mode.SaveConfig(cfgPath, existing); serr != nil {
				fmt.Fprintf(out, "  ⚠ save daemon config: %v\n", serr)
			} else {
				fmt.Fprintf(out, "  mode:    %s\n", selectedMode)
			}

			opts := service.Options{
				BinPath:    bin,
				SocketPath: layout.SocketPath,
				LogDir:     layout.LogDir,
			}

			st, err := service.Install(opts)
			if err != nil {
				fmt.Fprintf(out, "✗ install failed: %v\n", err)
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Try 'grafel install --foreground' to run the daemon directly")
				fmt.Fprintln(out, "and see error output.")
				return err
			}

			pidStr := ""
			if st.PID > 0 {
				pidStr = fmt.Sprintf(" pid=%d", st.PID)
			}
			fmt.Fprintf(out, "✓ grafel daemon installed and running%s\n", pidStr)
			fmt.Fprintf(out, "  socket:  %s\n", opts.SocketPath)
			fmt.Fprintf(out, "  service: %s\n", st.UnitFile)

			// Register grafel MCP bridge in every detected Claude Code
			// config dir (primary ~/.claude.json + any ~/.claude-*/). Per
			// ADR-0017 #827 the bridge translates MCP JSON-RPC 2.0 from
			// Claude Code to the daemon's JSON-RPC 1.0 socket. Failures are
			// soft — we report them but do not abort the install.
			registerMCPInClaudeConfigs(out, bin, claudeConfigDirs)

			// Symlink the 6 grafel skills into every detected Claude Code
			// config directory's skills/ subdirectory. This allows Claude Code
			// to discover and run the skills directly (e.g. /grafel-graph-quality).
			// Failures are soft — we report them but do not abort the install.
			if !skipSkillLink {
				installSkillsInClaudeConfigs(out, bin, skillsSourceDir, claudeConfigDirs)
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false,
		"skip service registration; run the daemon directly in this terminal (debug mode)")
	cmd.Flags().StringSliceVar(&claudeConfigDirs, "claude-config-dirs", nil,
		"explicit list of .claude.json paths to register MCP in (default: auto-detect ~/.claude.json + ~/.claude-*/)")
	cmd.Flags().StringVar(&skillsSourceDir, "skills-source-dir", "",
		"override the skills directory location (default: auto-detect from binary location or dev paths)")
	cmd.Flags().BoolVar(&skipSkillLink, "skip-skill-link", false,
		"skip symlinking skills into Claude Code's skills/ directories (legacy path only)")
	cmd.Flags().StringVar(&installMode, "mode", "",
		"operational mode: background (default), workstation, or readonly")
	// #2210: COPY mode flags.
	cmd.Flags().BoolVar(&copyMode, "copy", true,
		"run the full atomic COPY-mode install transaction (copies skills, registers MCP, restarts daemon, updates .gitignore, writes install.json)")
	// #2212: DEV mode flag.
	cmd.Flags().BoolVar(&devMode, "dev", false,
		"run the DEV-mode install transaction: symlinks skills from the repo working tree instead of copying them (for contributors; --dev takes precedence over --copy)")
	cmd.Flags().BoolVar(&force, "force", false,
		"bypass the partial-install guard; use after a failed install or 'grafel uninstall && grafel install'")
	// #2222: git hooks opt-out.
	cmd.Flags().BoolVar(&noHooks, "no-hooks", false,
		"skip automatic git hook installation (post-checkout, post-merge, post-rewrite, pre-push)")
	// #5256: per-tool selection.
	cmd.Flags().StringVar(&toolsCSV, "tools", "",
		"comma-separated AI coding tools to target (e.g. claude,cursor,windsurf); when set, selection is non-interactive. Run 'grafel tools list' for valid IDs")
	cmd.Flags().BoolVar(&noWizard, "no-wizard", false,
		"skip the interactive tool-selection wizard even on a TTY (keep the current/default tool set)")
	cmd.Flags().BoolVar(&assumeYes, "yes", false,
		"assume defaults for all prompts (alias for --no-wizard for tool selection); never blocks automation")
	// Curl-installer support: record the running binary in install.json and do
	// nothing else. Not a full install — see internal/install/refreshstate.go.
	cmd.Flags().BoolVar(&refreshState, "refresh-state", false,
		"only re-record this binary's path and checksum in ~/.grafel/install.json (no daemon restart, no skills, no MCP, no git changes); used by the curl installer after an in-place upgrade")
	// Curl-installer support (#6169): register the MCP server and do nothing
	// else. Not a full install — see internal/install/registermcp.go.
	cmd.Flags().BoolVar(&registerMCPOnly, "register-mcp", false,
		"only register this binary as the grafel MCP server in the detected host configs, recording it in ~/.grafel/install.json and a timestamped pre-change copy of each config under ~/.grafel/backups/mcpreg/ (no daemon restart, no skills, no git changes); used by the installers to finish a first-ever install")
	return cmd
}

// refreshStateOnlyFlags is the set of install flags that ask for work neither
// --refresh-state nor --register-mcp performs. Shared so the two narrow modes
// cannot drift apart as flags are added.
var refreshStateOnlyFlags = []string{
	"foreground", "claude-config-dirs", "skills-source-dir", "skip-skill-link",
	"mode", "copy", "dev", "force", "no-hooks", "tools", "no-wizard", "yes",
}

// conflictingFlags returns the names of the given flags the user explicitly
// typed. Only flags with Changed set count, so the --copy default of true is
// not a conflict.
func conflictingFlags(cmd *cobra.Command, names []string) []string {
	var conflicts []string
	for _, name := range names {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			conflicts = append(conflicts, "--"+name)
		}
	}
	return conflicts
}

// conflictingRefreshStateFlags returns the names of any explicitly-set install
// flags that --refresh-state cannot honour.
//
// --register-mcp is included: --refresh-state records a checksum and registers
// nothing, so honouring only the former for a user who asked for both would
// report an install that did not happen.
func conflictingRefreshStateFlags(cmd *cobra.Command) []string {
	return conflictingFlags(cmd, append(append([]string(nil), refreshStateOnlyFlags...), "register-mcp"))
}

// conflictingRegisterMCPFlags returns the names of any explicitly-set install
// flags that --register-mcp cannot honour.
//
// --claude-config-dirs is the one exception and is deliberately absent from the
// list: it names the host configs to write, which is precisely what this mode
// does.
//
// --refresh-state is NOT listed, and that is not an oversight: the
// --refresh-state branch is evaluated first in RunE, so `--register-mcp
// --refresh-state` never reaches this function — conflictingRefreshStateFlags
// rejects it, and that rejection is what TestInstallRefreshState_RejectsRegisterMCP
// pins. Listing it here would be unreachable code that reads like a guard.
func conflictingRegisterMCPFlags(cmd *cobra.Command) []string {
	var names []string
	for _, n := range refreshStateOnlyFlags {
		if n == "claude-config-dirs" {
			continue
		}
		names = append(names, n)
	}
	return conflictingFlags(cmd, names)
}

// runRegisterMCP executes the narrow MCP registration and prints a summary.
//
// A host that could not be written is reported and the command still succeeds:
// the installer calls this best-effort, and one unwritable config is not a
// reason to fail an install whose other hosts are now working. An error is
// returned only when nothing could be done at all.
func runRegisterMCP(out io.Writer, claudeConfigDirs []string) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	res, err := install.RegisterMCP(install.RegisterMCPOptions{
		BinPath:          bin,
		ClaudeConfigDirs: claudeConfigDirs,
	})
	if err != nil {
		return err
	}
	for _, f := range res.Failed {
		fmt.Fprintf(out, "  ⚠ MCP register %s: %v\n", f.Path, f.Err)
	}
	if len(res.Registered) == 0 {
		// Not an error: a machine with no MCP host installed is a legitimate
		// state (server, CI runner). Say so plainly instead of claiming success.
		fmt.Fprintln(out, "no MCP host configs detected — nothing to register")
		return nil
	}
	fmt.Fprintf(out, "✓ grafel MCP registered in:\n")
	for _, p := range res.Registered {
		fmt.Fprintf(out, "    %s\n", p)
	}
	fmt.Fprintln(out, "  Restart Claude Code to load the grafel MCP tools.")
	return nil
}

// runRefreshState executes the narrow install.json CLI-record refresh and
// prints a one-line summary. It never mutates anything outside install.json.
func runRefreshState(out io.Writer) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	res, err := install.RefreshState(install.RefreshOptions{BinPath: bin})
	if err != nil {
		return err
	}
	// GRAFEL_REFRESH_STATE_QUIET: `grafel update` re-invokes this command as a
	// child purely to reach the watcher reconcile below (#6179 F1) and forwards
	// its output. The install-state bookkeeping lines are noise in that context
	// — update prints its own version summary — but the watcher summary is the
	// whole reason the child exists, so quiet mode suppresses only the former.
	if os.Getenv(refreshStateQuietEnv) == "" {
		switch {
		case !res.HadState:
			// Nothing installed yet: quick-doctor is already silent in this
			// state, and fabricating an install.json here would make `grafel
			// doctor` report skills/MCP drift that does not exist.
			fmt.Fprintln(out, "no ~/.grafel/install.json to refresh — run 'grafel install' to install grafel")
		case !res.Changed:
			fmt.Fprintf(out, "install state already current (%s)\n", res.Path)
		default:
			fmt.Fprintf(out, "✓ install state refreshed: %s (sha256 %s…)\n", res.Path, res.SHA256[:12])
		}
	}

	// ── watcher-unit reconcile (#6179 F1) ──────────────────────────────────
	// This is the ONE hook the curl installer already calls with the NEWLY
	// placed binary (install.sh's record_install_state → `grafel install
	// --refresh-state`), which makes it the only place a template fix can
	// reach an existing install automatically. Without it, an upgrade leaves
	// every already-written watcher unit carrying the old settings —
	// unconditional KeepAlive, no ThrottleInterval — and the storm #6179
	// reports continues after the "fix" ships.
	//
	// It stays inside RefreshState's spirit rather than violating it: the
	// argument in internal/install/refreshstate.go is that the installer must
	// not mutate a repository the user never mentioned, restart the daemon, or
	// rewrite .claude.json. This touches none of those — only grafel's OWN
	// already-registered watcher units, only the ones whose rendered content
	// actually differs, and it is a pure read (no writes, no launchctl) on a
	// machine that is already current.
	reconcileWatcherUnits(out, bin)
	return nil
}

// reconcileWatcherUnits repairs stale watcher unit files and reports what it
// did. Best-effort by design: this is a repair bolted onto an unrelated
// command, so it must never turn a successful --refresh-state into a failure.
// It is a package var so tests can observe the wiring without a real registry.
var reconcileWatcherUnits = func(out io.Writer, bin string) {
	res, err := install.ReconcileWatcherUnits(install.ReconcileWatcherOptions{BinPath: bin})
	if err != nil {
		fmt.Fprintf(out, "watcher units: could not reconcile: %v\n", err)
		return
	}
	printReconcileSummary(out, res)
}

// refreshStateQuietEnv suppresses --refresh-state's install-state bookkeeping
// lines while keeping the watcher-unit summary. Set by `grafel update` on the
// child it spawns; not a user-facing knob.
const refreshStateQuietEnv = "GRAFEL_REFRESH_STATE_QUIET"

// printReconcileSummary reports what the watcher reconcile did.
//
// It says the notification burst is coming (#6179 F1-b). On a machine where
// every unit is stale — precisely the machine #6179 was reported from — the
// one-time repair re-registers all of them, and each re-registration is a
// launchctl bootout+bootstrap that macOS posts a Background Items notification
// for. Silence there would look exactly like the bug recurring the moment the
// user upgrades, so the burst has to be announced rather than merely tolerated.
//
// Silent when nothing was stale: that is the steady state and it deserves no
// output at all.
func printReconcileSummary(out io.Writer, res *install.ReconcileWatcherResult) {
	if res == nil || (len(res.Rewritten) == 0 && len(res.Migrated) == 0 && res.Absent == 0) {
		return
	}
	if len(res.Rewritten) > 0 || len(res.Migrated) > 0 {
		fmt.Fprintf(out, "✓ watcher units refreshed: %d rewritten, %d re-registered, %d already current\n",
			len(res.Rewritten), len(res.Reloaded), res.Current)
	}
	if res.Absent > 0 {
		// #6183 F2: a repo that is registered with watchers on but has no unit
		// under either label used to be counted here and never mentioned. That
		// silence is what made an interrupted migration unrecoverable in
		// practice — the operator had no signal at all that a repo was
		// unwatched. Reconcile still will not create these (that is Apply's
		// job), so the remedy has to be said out loud.
		fmt.Fprintf(out, "  %d registered repo(s) have watchers enabled but no watcher unit "+
			"installed; run `grafel install` for their group to add one\n", res.Absent)
	}
	if len(res.Migrated) > 0 {
		// #6183: the label now includes a path digest, so these repos' units
		// moved to a new filename. Say so — a user who greps LaunchAgents for
		// the old name after upgrading would otherwise conclude their watchers
		// had been deleted.
		fmt.Fprintf(out, "  %d watcher unit(s) were renamed to make repos with identical directory "+
			"names distinguishable (issue #6183); the previous units were deregistered and removed\n",
			len(res.Migrated))
	}
	if len(res.Reloaded) > 0 {
		// Re-registering a unit RE-ACTIVATES it, including one a previous
		// `grafel stop` deactivated (Loader.Load clears the persisted
		// disable). Saying so is the difference between an acceptable
		// side effect and a silent one: `grafel update` is run to get a new
		// binary, not to restart watchers, so a user who stopped grafel a week
		// ago must be told that some of it is running again.
		fmt.Fprintf(out, "  note: re-activated %d repo watcher(s) — if you had run 'grafel stop', "+
			"run it again to stop them\n", len(res.Reloaded))
		fmt.Fprintf(out, "  macOS may show up to %d Background Items notifications while these "+
			"re-register — this is the one-time repair for issue #6179, not a recurrence\n",
			len(res.Reloaded))
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "  warning: %s\n", w)
	}
}

// resolveToolSelection decides the per-tool selection for `grafel install`.
//
// Returns (selection, applied, err):
//   - (ids, true, nil)  → caller should persist ids to GroupConfig.Tools.
//   - (nil, false, nil) → no change requested (no flag, no TTY, or --yes/
//     --no-wizard): leave the existing selection alone so back-compat /
//     automation behaviour is preserved.
//
// --tools wins and is non-interactive. Otherwise the wizard runs ONLY when
// stdin is an interactive terminal and the user did not pass --yes/--no-wizard.
func resolveToolSelection(cmd *cobra.Command, out io.Writer, toolsCSV string, noWizard, assumeYes bool) ([]string, bool, error) {
	if toolsCSV != "" {
		ids, err := tooladapter.ParseToolsFlag(toolsCSV)
		if err != nil {
			return nil, false, err
		}
		return ids, true, nil
	}
	if noWizard || assumeYes {
		return nil, false, nil
	}
	if !stdinIsTTY() {
		// Non-interactive / piped / CI: never prompt.
		return nil, false, nil
	}
	ids, err := runToolWizard(out)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

// stdinIsTTY reports whether standard input is an interactive terminal. It is
// a var so tests can stub it.
var stdinIsTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// runToolWizard presents the interactive multi-select checkbox of all
// adapters, pre-checked by DetectInstalled(), and returns the chosen IDs.
// The selection→IDs mapping is delegated to tooladapter.NormalizeSelection so
// the pure logic is testable without a TTY.
func runToolWizard(out io.Writer) ([]string, error) {
	choices := tooladapter.WizardChoices(nil)
	opts := make([]huh.Option[string], 0, len(choices))
	var preselected []string
	for _, c := range choices {
		label := c.DisplayName
		if c.Detected {
			label += " (detected)"
		}
		opts = append(opts, huh.NewOption(label, c.ID).Selected(c.PreChecked))
		if c.PreChecked {
			preselected = append(preselected, c.ID)
		}
	}
	selected := append([]string{}, preselected...)
	if err := huh.NewMultiSelect[string]().
		Title("AI coding tools to target").
		Description("Pre-checked tools were detected on this machine. Toggle with space, confirm with enter.").
		Options(opts...).
		Value(&selected).
		Run(); err != nil {
		return nil, err
	}
	ids := tooladapter.NormalizeSelection(selected)
	if len(ids) == 0 {
		fmt.Fprintln(out, "  no tools selected — keeping the default (all supported tools)")
		// Persist an empty explicit set would disable everything; instead we
		// treat "selected nothing" as "use the default" to avoid a footgun.
		return tooladapter.AllIDs(), nil
	}
	return ids, nil
}

// persistToolSelection writes the resolved tool IDs into GroupConfig.Tools for
// every registered group and re-applies the per-tool artifact delta in-process
// (no subprocess, no daemon restart). With no groups registered it is a no-op
// (the daemon-service install still proceeds).
func persistToolSelection(out io.Writer, ids []string) error {
	groups, err := registry.Groups()
	if err != nil {
		return fmt.Errorf("read registry: %w", err)
	}
	if len(groups) == 0 {
		// #5701 ordering footgun: no group exists yet, so there is nothing to
		// write Tools into. Stash the selection so the next `wizard`/`group add`
		// picks it up (consumePendingTools in applyGroupConfig) instead of
		// silently dropping it and re-defaulting to all tools.
		if err := savePendingTools(ids); err != nil {
			fmt.Fprintf(out, "  ⚠ tools: could not stash selection: %v\n", err)
			return nil
		}
		fmt.Fprintf(out, "  tools:   %v (stashed; applied on first group registration)\n", ids)
		return nil
	}
	bin, _ := os.Executable()
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil || cfg == nil {
			fmt.Fprintf(out, "  ⚠ tools: load %s: %v\n", g.Name, err)
			continue
		}
		prev := tooladapter.EnabledTools(cfg)
		cfg.Tools = ids
		if err := registry.SaveGroupConfig(g.ConfigPath, cfg); err != nil {
			fmt.Fprintf(out, "  ⚠ tools: save %s: %v\n", g.Name, err)
			continue
		}
		res, err := install.ApplyToolDelta(cfg, g.Name, bin, prev, ids, nil)
		if err != nil {
			fmt.Fprintf(out, "  ⚠ tools: apply %s: %v\n", g.Name, err)
			continue
		}
		fmt.Fprintf(out, "  tools:   %s → %v (enabled %v, disabled %v)\n",
			g.Name, ids, res.Enabled, res.Disabled)
	}
	return nil
}

// runInstallDev runs the DEV-mode install transaction (issue #2212) and
// prints a structured summary. Called from newInstallCmd when --dev is set.
//
// It warns the user when they are switching from a previous COPY install,
// because the mode switch removes the old COPY skills and replaces them
// with symlinks.  The user is advised that `grafel uninstall &&
// grafel install --dev` is the one-command mode switch.
func runInstallDev(out io.Writer, opts install.DevOptions) error {
	result, err := install.RunDev(opts)
	if err != nil {
		fmt.Fprintf(out, "✗ install (dev mode) failed: %v\n", err)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Run 'grafel install --dev --force' to retry, or")
		fmt.Fprintln(out, "'grafel uninstall && grafel install --dev' to start clean.")
		return err
	}

	fmt.Fprintf(out, "✓ grafel installed (dev/symlink mode)\n")
	fmt.Fprintf(out, "  binary:  %s\n", result.CLIPath)
	if len(result.CLISHA256) >= 16 {
		fmt.Fprintf(out, "  sha256:  %s...\n", result.CLISHA256[:16])
	}
	if len(result.SkillsLinked) > 0 {
		fmt.Fprintf(out, "  skills:  %d symlinked (live links to repo working tree)\n", len(result.SkillsLinked))
	}
	if len(result.SkillsFallbackCopied) > 0 {
		fmt.Fprintf(out, "  skills:  %d fell back to COPY (symlink not available — privilege required?): %v\n",
			len(result.SkillsFallbackCopied), result.SkillsFallbackCopied)
	}
	if len(result.MCPPaths) > 0 {
		fmt.Fprintf(out, "  MCP:     registered in %d config file(s)\n", len(result.MCPPaths))
		fmt.Fprintln(out, "           Restart Claude Code to load the grafel MCP tools.")
	}
	if result.DaemonVersion != "" {
		fmt.Fprintf(out, "  daemon:  %s\n", result.DaemonVersion)
	}
	if result.GitignoreRepo != "" {
		fmt.Fprintf(out, "  .gitignore: /.grafel/ added in %s\n", result.GitignoreRepo)
	}
	if result.StatePath != "" {
		fmt.Fprintf(out, "  state:   %s\n", result.StatePath)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Tip: to switch back to copy mode, run:")
	fmt.Fprintln(out, "       grafel uninstall && grafel install")
	return nil
}

// runInstallCopy runs the COPY-mode install transaction (issue #2210) and
// prints a structured summary. Called from newInstallCmd when --copy is set.
func runInstallCopy(out io.Writer, opts install.CopyOptions) error {
	// opts.Intent is deliberately NOT forced here: it is the caller's
	// declaration, and RunCopy rejects it unset (#6162). Setting it in this
	// helper would silently launder any future caller that had not thought
	// about whether it may modify the user's repository.
	result, err := install.RunCopy(opts)
	if err != nil {
		fmt.Fprintf(out, "✗ install (copy mode) failed: %v\n", err)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Run 'grafel install --force' to retry, or")
		fmt.Fprintln(out, "'grafel uninstall && grafel install' to start clean.")
		return err
	}

	fmt.Fprintf(out, "✓ grafel installed (copy mode)\n")
	fmt.Fprintf(out, "  binary:  %s\n", result.CLIPath)
	if len(result.CLISHA256) >= 16 {
		fmt.Fprintf(out, "  sha256:  %s...\n", result.CLISHA256[:16])
	}
	if len(result.SkillsInstalled) > 0 {
		fmt.Fprintf(out, "  skills:  %d copied\n", len(result.SkillsInstalled))
	}
	if len(result.MCPPaths) > 0 {
		fmt.Fprintf(out, "  MCP:     registered in %d config file(s)\n", len(result.MCPPaths))
		fmt.Fprintln(out, "           Restart Claude Code to load the grafel MCP tools.")
	}
	if result.DaemonVersion != "" {
		fmt.Fprintf(out, "  daemon:  %s\n", result.DaemonVersion)
	}
	if result.GitignoreRepo != "" {
		fmt.Fprintf(out, "  .gitignore: /.grafel/ added in %s\n", result.GitignoreRepo)
	}
	if result.StatePath != "" {
		fmt.Fprintf(out, "  state:   %s\n", result.StatePath)
	}
	return nil
}

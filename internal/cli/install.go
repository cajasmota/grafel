package cli

import (
	"fmt"
	"io"
	"os"

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
	return cmd
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

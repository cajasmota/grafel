// uninstall.go implements `grafel uninstall` (issue #2213).
//
// Uninstall is the symmetric inverse of RunCopy / RunDev:
//  1. Read install.json for the list of owned skills and MCP paths.
//  2. Remove copied/linked skills from ~/.claude/skills/<name>/ (only those in install.json).
//  3. Deregister grafel from MCP in every registered .claude.json.
//  4. Stop the daemon and tear down the OS service (launchd/systemd/schtasks
//     unit, socket, pidfile) via service.Uninstall.
//  5. Default: leave the installed CLI binary in place so a subsequent
//     install/start works without re-downloading or rebuilding (#4478).
//     --remove-binary: also remove the CLI binary (with confirmation prompt
//     unless --yes).
//  6. Remove ~/.grafel/install.json.
//  7. Default: leave ~/.grafel/store/ intact.
//     --purge: also remove every grafel-created scratch dir under the prefix
//     (store/, docs/, backups/, logs/, sockets/) and, if the ~/.grafel root is
//     then empty, the root itself — foreign (non-grafel) content is preserved.
//
// All steps are idempotent: missing files are silently skipped.
// If no install.json is found the command exits 0 (nothing to do).
package install

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/service"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/skilllink"
	"github.com/cajasmota/grafel/internal/registry"
)

// UninstallOptions controls RunUninstall behaviour.
type UninstallOptions struct {
	// StatePath is the path for install.json.
	// Defaults to DefaultStatePath().
	StatePath string

	// Purge, when true, also removes every grafel-created scratch dir under
	// the prefix (store/, docs/, backups/, logs/, sockets/) in addition to the
	// install artifacts, and removes the ~/.grafel root itself when it is left
	// empty (foreign content is preserved).
	Purge bool

	// RemoveBinary, when true, removes the installed CLI binary as part of
	// uninstall. The default (false) leaves the binary in place so a
	// subsequent `install`/`start` works without re-downloading or
	// rebuilding it (#4478). Service artifacts (unit/socket/pidfile) are torn
	// down regardless of this flag.
	RemoveBinary bool

	// Yes skips the confirmation prompt before removing the CLI binary.
	// It only has an effect when RemoveBinary is true.
	Yes bool

	// DryRun prints actions without writing anything.
	DryRun bool

	// SkipDaemonStop skips the daemon stop step (useful in tests where no
	// real daemon is running).
	SkipDaemonStop bool

	// ConfirmFn is an injectable confirmation function. When nil the
	// production implementation (promptConfirm) is used.
	// Returns true if the user confirmed, false to abort.
	ConfirmFn func(prompt string) (bool, error)

	// registeredRootFn resolves the root the LIVE daemon serves (the HOME
	// baked into the installed OS service unit). When nil it defaults to
	// service.RegisteredRoot. Injectable so the #5277 guard decision can be
	// unit-tested without reading a real launchd plist / systemd unit.
	registeredRootFn func() (root string, found bool, err error)

	// stopDaemonFn performs the actual service teardown. When nil it defaults
	// to a wrapper over service.Uninstall. Injectable so tests can assert
	// whether the stop was performed WITHOUT ever invoking real launchctl /
	// systemctl / schtasks.
	stopDaemonFn func() error

	// targetRootFn resolves the root of the install being uninstalled. When
	// nil it defaults to uninstallTargetRoot (HOME). Injectable for tests.
	targetRootFn func() string

	// isolatedHomeFn reports whether GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1.
	// When nil it defaults to isolatedHomeActive. Injectable for tests.
	isolatedHomeFn func() bool

	// warnFn receives WARN lines (e.g. when the daemon stop is skipped for
	// isolation safety). When nil it writes to os.Stderr.
	warnFn func(string)

	// groupsFn / loadGroupFn resolve registered groups + their configs so the
	// MCP deregistration sweep covers EVERY enabled tool's own config file
	// (Cursor/Windsurf/Codex/Kiro), not just the recorded .claude.json paths
	// (#5258). When nil they default to registry.Groups /
	// registry.LoadGroupConfig. Injectable for tests.
	groupsFn    func() ([]registry.GroupRef, error)
	loadGroupFn func(path string) (*registry.GroupConfig, error)

	// unregisterToolFn removes grafel's MCP entry from a tool's own config
	// file (per-format: JSON or Codex TOML), preserving foreign entries. When
	// nil it defaults to mcpreg.Unregister. Injectable so tests assert the
	// multi-tool sweep without touching live config files.
	unregisterToolFn func(tool mcpreg.Tool) error
}

// UninstallResult reports what RunUninstall accomplished.
type UninstallResult struct {
	// SkillsRemoved lists the skill names removed from ~/.claude/skills/.
	SkillsRemoved []string

	// MCPPaths lists the .claude.json files updated.
	MCPPaths []string

	// MCPToolsDeregistered lists the per-tool MCP configs (Cursor/Windsurf/
	// Codex/Kiro/…) grafel's entry was removed from beyond the recorded
	// .claude.json paths (#5258).
	MCPToolsDeregistered []mcpreg.Tool

	// DaemonStopped is true when the daemon was stopped.
	DaemonStopped bool

	// BinaryRemoved is true when the CLI binary was removed.
	BinaryRemoved bool

	// StateRemoved is true when install.json was removed.
	StateRemoved bool

	// StoreRemoved is true when ~/.grafel/store/ was removed (--purge only).
	StoreRemoved bool

	// DocsRemoved is true when ~/.grafel/docs/ was removed (--purge only).
	DocsRemoved bool

	// PurgedDirs lists the grafel-created scratch subdirectories removed under
	// the prefix during --purge (store/, docs/, backups/, logs/, sockets/).
	PurgedDirs []string

	// RootRemoved is true when the ~/.grafel root directory itself was removed
	// after purge because it was left empty (no foreign content). It stays
	// false when the root still held foreign (non-grafel) content, which is
	// preserved (--purge only).
	RootRemoved bool
}

// grafelScratchDirs are the subdirectories grafel creates under the install
// prefix (~/.grafel). --purge removes all of these. Keep in sync with the
// daemon/install paths: store/ + docs/ (graph + generated docs), backups/
// (incl. backups/mcpreg/ config snapshots), logs/ (daemon logs), sockets/
// (unix socket dir).
var grafelScratchDirs = []string{"store", "docs", "backups", "logs", "sockets"}

func (o *UninstallOptions) applyDefaults() error {
	if o.StatePath == "" {
		p, err := DefaultStatePath()
		if err != nil {
			return err
		}
		o.StatePath = p
	}
	// Auto-confirm when stdin is not an interactive terminal (scripts, CI,
	// agents). Without this, the binary-removal prompt blocks forever on a
	// closed/piped stdin (issue #4462). An explicit --yes always wins; this
	// only flips the default for the non-interactive case. We only apply the
	// auto-yes when no custom ConfirmFn was injected — an explicit ConfirmFn
	// means the caller (e.g. a test, or a wrapper) wants to drive the decision
	// itself.
	if o.ConfirmFn == nil {
		if !o.Yes && !stdinIsTTY() {
			o.Yes = true
		}
		o.ConfirmFn = promptConfirm
	}
	if o.registeredRootFn == nil {
		o.registeredRootFn = service.RegisteredRoot
	}
	if o.stopDaemonFn == nil {
		o.stopDaemonFn = func() error { return service.Uninstall(service.Options{}) }
	}
	if o.targetRootFn == nil {
		o.targetRootFn = uninstallTargetRoot
	}
	if o.isolatedHomeFn == nil {
		o.isolatedHomeFn = isolatedHomeActive
	}
	if o.warnFn == nil {
		o.warnFn = func(msg string) {
			fmt.Fprintf(os.Stderr, "grafel uninstall: WARN: %s\n", msg)
		}
	}
	if o.unregisterToolFn == nil {
		o.unregisterToolFn = mcpreg.Unregister
	}
	return nil
}

// stdinIsTTY reports whether os.Stdin is an interactive terminal. When it is
// not (pipe, redirect, closed fd — typical of CI/agent/scripted runs), the
// uninstall must not block on a confirmation prompt.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// RunUninstall executes the uninstall transaction.
// It is idempotent: missing files are silently skipped.
func RunUninstall(opts UninstallOptions) (*UninstallResult, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	result := &UninstallResult{}

	// ── read install state ────────────────────────────────────────────────────
	state, err := ReadState(opts.StatePath)
	if err != nil {
		return nil, fmt.Errorf("read install state: %w", err)
	}
	if state == nil {
		// Not installed — idempotent success.
		return result, nil
	}

	// ── resolve skills destinations (every registered Claude config) ──────────
	skillsDestDirs := resolveAllSkillsDestDirs(state)

	// ── Step 1: Remove skills ─────────────────────────────────────────────────
	removedSet := map[string]bool{}
	for _, skillsDestDir := range skillsDestDirs {
		if skillsDestDir == "" {
			continue
		}
		for skillName := range state.Skills {
			dst := filepath.Join(skillsDestDir, skillName)
			if opts.DryRun {
				fmt.Fprintf(os.Stderr, "grafel uninstall (dry-run): would remove skill %s\n", dst)
				removedSet[skillName] = true
				continue
			}
			if _, err := os.Lstat(dst); err == nil {
				if err := os.RemoveAll(dst); err != nil {
					fmt.Fprintf(os.Stderr, "grafel uninstall: remove skill %s: %v\n", dst, err)
				} else {
					removedSet[skillName] = true
				}
			}
		}
	}
	for skillName := range removedSet {
		result.SkillsRemoved = append(result.SkillsRemoved, skillName)
	}

	// ── Step 2: Deregister MCP ────────────────────────────────────────────────
	for _, cfgPath := range state.MCP.RegisteredPaths {
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "grafel uninstall (dry-run): would deregister MCP from %s\n", cfgPath)
			result.MCPPaths = append(result.MCPPaths, cfgPath)
			continue
		}
		if err := mcpreg.UnregisterPath(cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "grafel uninstall: deregister MCP %s: %v\n", cfgPath, err)
		} else {
			result.MCPPaths = append(result.MCPPaths, cfgPath)
		}
	}

	// ── Step 2b: Sweep every enabled tool's own MCP config (#5258) ────────────
	// Step 2 only removed grafel from the recorded .claude.json paths. The
	// enabled-tool set lives in each group's config, so resolve it here and
	// deregister grafel's entry from EVERY enabled tool's own config file —
	// Cursor (~/.cursor/mcp.json), Windsurf (~/.codeium/windsurf/mcp_config.json),
	// Codex (~/.codex/config.toml, TOML), Kiro (~/.kiro/settings/mcp.json).
	// mcpreg.Unregister dispatches per-format (JSON vs TOML) and removes ONLY
	// grafel's key/table, preserving every foreign server. Idempotent: a
	// missing file or absent entry is a no-op.
	bindings := resolveEnabledToolBindings(opts.groupsFn, opts.loadGroupFn)
	for _, tool := range mcpToolsFromBindings(bindings) {
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "grafel uninstall (dry-run): would deregister MCP for tool %s\n", tool)
			result.MCPToolsDeregistered = append(result.MCPToolsDeregistered, tool)
			continue
		}
		if err := opts.unregisterToolFn(tool); err != nil {
			fmt.Fprintf(os.Stderr, "grafel uninstall: deregister MCP for tool %s: %v\n", tool, err)
		} else {
			result.MCPToolsDeregistered = append(result.MCPToolsDeregistered, tool)
		}
	}

	// ── Step 3: Stop daemon (with #5277 isolation safety guard) ───────────────
	// The OS service label is GLOBAL. Before stopping/unregistering it we must
	// confirm the live daemon belongs to THIS uninstall target's root — never
	// tear down a daemon serving a different root (e.g. the developer's live
	// daemon while uninstalling an isolated sandbox install). evaluateDaemonStop
	// is pure; the I/O (reading the recorded root, performing the stop) is in
	// injectable hooks so the decision is unit-tested without touching launchctl.
	if !opts.SkipDaemonStop && !opts.DryRun {
		regRoot, regFound, regErr := opts.registeredRootFn()
		decision := evaluateDaemonStop(
			regRoot, regFound, regErr != nil,
			opts.targetRootFn(), opts.isolatedHomeFn(),
		)
		if !decision.Stop {
			opts.warnFn(decision.Reason)
		} else if err := opts.stopDaemonFn(); err != nil {
			fmt.Fprintf(os.Stderr, "grafel uninstall: stop daemon: %v\n", err)
		} else {
			result.DaemonStopped = true
		}
	} else if opts.SkipDaemonStop || opts.DryRun {
		result.DaemonStopped = opts.DryRun
	}

	// ── Step 4: Remove CLI binary (opt-in, with confirmation) ─────────────────
	// By default the binary is KEPT so a subsequent install/start works without
	// re-downloading or rebuilding (#4478). Only --remove-binary deletes it.
	if opts.RemoveBinary && state.CLI.Path != "" {
		if _, err := os.Stat(state.CLI.Path); err == nil {
			removeIt := opts.Yes || opts.DryRun
			if !removeIt {
				confirmed, cerr := opts.ConfirmFn(
					fmt.Sprintf("Remove grafel binary %s? [y/N] ", state.CLI.Path))
				if cerr != nil {
					return nil, fmt.Errorf("confirmation prompt: %w", cerr)
				}
				removeIt = confirmed
			}

			if opts.DryRun {
				fmt.Fprintf(os.Stderr, "grafel uninstall (dry-run): would remove binary %s\n", state.CLI.Path)
				result.BinaryRemoved = true
			} else if removeIt {
				// removeBinary is self-delete-safe: on Windows it renames the
				// running exe aside and schedules it for deletion on reboot when
				// it cannot be unlinked directly (#5264). On Unix it is a plain
				// os.Remove.
				if err := removeBinary(state.CLI.Path); err != nil {
					fmt.Fprintf(os.Stderr, "grafel uninstall: remove binary %s: %v\n", state.CLI.Path, err)
				} else {
					result.BinaryRemoved = true
				}
			}
		}
	}

	// ── Step 5: Remove install.json ───────────────────────────────────────────
	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "grafel uninstall (dry-run): would remove %s\n", opts.StatePath)
		result.StateRemoved = true
	} else {
		if err := os.Remove(opts.StatePath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "grafel uninstall: remove install.json: %v\n", err)
		} else {
			result.StateRemoved = true
		}
	}

	// ── Step 6 (--purge): Remove grafel scratch dirs + empty root ─────────────
	// --purge removes EVERY grafel-created subdirectory under the prefix —
	// store/, docs/, backups/ (incl. the mcpreg config snapshots), logs/, and
	// sockets/ — not just store/+docs/ (the leftover gap from #5274). It then
	// attempts to remove the ~/.grafel root itself, but ONLY when it is empty:
	// os.Remove (non-recursive) succeeds on an empty dir and fails on a
	// non-empty one, so any FOREIGN content a user placed under the prefix is
	// preserved. install.json/store/docs/backups/logs/sockets were each removed
	// individually above, so an empty root means "grafel artifacts only".
	if opts.Purge {
		grafelDir := filepath.Dir(opts.StatePath)

		for _, name := range grafelScratchDirs {
			p := filepath.Join(grafelDir, name)
			if opts.DryRun {
				fmt.Fprintf(os.Stderr, "grafel uninstall --purge (dry-run): would remove %s\n", p)
				result.PurgedDirs = append(result.PurgedDirs, name)
				continue
			}
			if err := os.RemoveAll(p); err != nil {
				fmt.Fprintf(os.Stderr, "grafel uninstall --purge: remove %s: %v\n", p, err)
			} else {
				result.PurgedDirs = append(result.PurgedDirs, name)
			}
		}

		// Back-compat: keep the existing StoreRemoved/DocsRemoved flags.
		for _, name := range result.PurgedDirs {
			switch name {
			case "store":
				result.StoreRemoved = true
			case "docs":
				result.DocsRemoved = true
			}
		}

		// Attempt to remove the now-(hopefully-)empty root. Non-recursive
		// os.Remove only succeeds when the directory is empty — foreign content
		// keeps the root in place and is left untouched.
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "grafel uninstall --purge (dry-run): would remove %s if empty\n", grafelDir)
			result.RootRemoved = true
		} else if err := os.Remove(grafelDir); err != nil {
			if !os.IsNotExist(err) {
				// ENOTEMPTY (foreign content) or other error: keep the root.
				opts.warnFn(fmt.Sprintf(
					"--purge: kept %s (not empty — preserving non-grafel content)", grafelDir))
			}
		} else {
			result.RootRemoved = true
		}
	}

	return result, nil
}

// resolveAllSkillsDestDirs returns the skills directory for every Claude
// config recorded in state.MCP.RegisteredPaths.  Each entry is mapped via
// skilllink.ClaudeSkillsDirForConfig so primary vs sidecar layouts are
// handled correctly.
func resolveAllSkillsDestDirs(state *State) []string {
	out := make([]string, 0, len(state.MCP.RegisteredPaths))
	for _, cfgPath := range state.MCP.RegisteredPaths {
		if d := skilllink.ClaudeSkillsDirForConfig(cfgPath); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// promptConfirm is the production confirmation prompt.
// It reads a line from stdin and returns true for "y" or "Y".
func promptConfirm(prompt string) (bool, error) {
	fmt.Fprint(os.Stdout, prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

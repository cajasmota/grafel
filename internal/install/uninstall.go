// uninstall.go implements `archigraph uninstall` (issue #2213).
//
// Uninstall is the symmetric inverse of RunCopy / RunDev:
//  1. Read install.json for the list of owned skills and MCP paths.
//  2. Remove copied/linked skills from ~/.claude/skills/<name>/ (only those in install.json).
//  3. Deregister archigraph from MCP in every registered .claude.json.
//  4. Stop the daemon gracefully via service.Uninstall.
//  5. Remove the CLI binary (with confirmation prompt unless --yes).
//  6. Remove ~/.archigraph/install.json.
//  7. Default: leave ~/.archigraph/store/ intact.
//     --purge: also remove ~/.archigraph/store/ and ~/.archigraph/docs/.
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

	"github.com/cajasmota/archigraph/internal/daemon/service"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// UninstallOptions controls RunUninstall behaviour.
type UninstallOptions struct {
	// StatePath is the path for install.json.
	// Defaults to DefaultStatePath().
	StatePath string

	// Purge, when true, also removes ~/.archigraph/store/ and
	// ~/.archigraph/docs/ in addition to the install artifacts.
	Purge bool

	// Yes skips the confirmation prompt before removing the CLI binary.
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
}

// UninstallResult reports what RunUninstall accomplished.
type UninstallResult struct {
	// SkillsRemoved lists the skill names removed from ~/.claude/skills/.
	SkillsRemoved []string

	// MCPPaths lists the .claude.json files updated.
	MCPPaths []string

	// DaemonStopped is true when the daemon was stopped.
	DaemonStopped bool

	// BinaryRemoved is true when the CLI binary was removed.
	BinaryRemoved bool

	// StateRemoved is true when install.json was removed.
	StateRemoved bool

	// StoreRemoved is true when ~/.archigraph/store/ was removed (--purge only).
	StoreRemoved bool

	// DocsRemoved is true when ~/.archigraph/docs/ was removed (--purge only).
	DocsRemoved bool
}

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
				fmt.Fprintf(os.Stderr, "archigraph uninstall (dry-run): would remove skill %s\n", dst)
				removedSet[skillName] = true
				continue
			}
			if _, err := os.Lstat(dst); err == nil {
				if err := os.RemoveAll(dst); err != nil {
					fmt.Fprintf(os.Stderr, "archigraph uninstall: remove skill %s: %v\n", dst, err)
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
			fmt.Fprintf(os.Stderr, "archigraph uninstall (dry-run): would deregister MCP from %s\n", cfgPath)
			result.MCPPaths = append(result.MCPPaths, cfgPath)
			continue
		}
		if err := mcpreg.UnregisterPath(cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph uninstall: deregister MCP %s: %v\n", cfgPath, err)
		} else {
			result.MCPPaths = append(result.MCPPaths, cfgPath)
		}
	}

	// ── Step 3: Stop daemon ───────────────────────────────────────────────────
	if !opts.SkipDaemonStop && !opts.DryRun {
		if err := service.Uninstall(service.Options{}); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph uninstall: stop daemon: %v\n", err)
		} else {
			result.DaemonStopped = true
		}
	} else if opts.SkipDaemonStop || opts.DryRun {
		result.DaemonStopped = opts.DryRun
	}

	// ── Step 4: Remove CLI binary (with confirmation) ─────────────────────────
	if state.CLI.Path != "" {
		if _, err := os.Stat(state.CLI.Path); err == nil {
			removeIt := opts.Yes || opts.DryRun
			if !removeIt {
				confirmed, cerr := opts.ConfirmFn(
					fmt.Sprintf("Remove archigraph binary %s? [y/N] ", state.CLI.Path))
				if cerr != nil {
					return nil, fmt.Errorf("confirmation prompt: %w", cerr)
				}
				removeIt = confirmed
			}

			if opts.DryRun {
				fmt.Fprintf(os.Stderr, "archigraph uninstall (dry-run): would remove binary %s\n", state.CLI.Path)
				result.BinaryRemoved = true
			} else if removeIt {
				if err := os.Remove(state.CLI.Path); err != nil {
					fmt.Fprintf(os.Stderr, "archigraph uninstall: remove binary %s: %v\n", state.CLI.Path, err)
				} else {
					result.BinaryRemoved = true
				}
			}
		}
	}

	// ── Step 5: Remove install.json ───────────────────────────────────────────
	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "archigraph uninstall (dry-run): would remove %s\n", opts.StatePath)
		result.StateRemoved = true
	} else {
		if err := os.Remove(opts.StatePath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "archigraph uninstall: remove install.json: %v\n", err)
		} else {
			result.StateRemoved = true
		}
	}

	// ── Step 6 (--purge): Remove store/ and docs/ ─────────────────────────────
	if opts.Purge {
		archigraphDir := filepath.Dir(opts.StatePath)

		storePath := filepath.Join(archigraphDir, "store")
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "archigraph uninstall --purge (dry-run): would remove %s\n", storePath)
			result.StoreRemoved = true
		} else {
			if err := os.RemoveAll(storePath); err != nil {
				fmt.Fprintf(os.Stderr, "archigraph uninstall --purge: remove store: %v\n", err)
			} else {
				result.StoreRemoved = true
			}
		}

		docsPath := filepath.Join(archigraphDir, "docs")
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "archigraph uninstall --purge (dry-run): would remove %s\n", docsPath)
			result.DocsRemoved = true
		} else {
			if err := os.RemoveAll(docsPath); err != nil {
				fmt.Fprintf(os.Stderr, "archigraph uninstall --purge: remove docs: %v\n", err)
			} else {
				result.DocsRemoved = true
			}
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

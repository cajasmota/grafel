// dev.go implements the DEV mode install transaction for `archigraph install --dev`.
//
// DEV mode is identical to COPY mode (copy.go) EXCEPT for step 2: instead of
// copying skill directories into ~/.claude/skills/<name>/, it creates symlinks
// (on macOS/Linux) or directory junctions (on Windows) that point directly into
// the archigraph repository working tree.  This means edits to skills in the
// repo are instantly visible to Claude Code without re-running install.
//
// Steps (identical step numbers to COPY mode):
//  1. CLI binary identification (SHA-256 of the running binary).
//  2. Skills symlink: os.Symlink(repo/skills/<name>, ~/.claude/skills/<name>).
//     On Windows, a junction is used. If junction creation fails (locked-down
//     machine), this step falls back to COPY mode with a printed warning.
//  3. MCP registration: write archigraph entry into all detected .claude.json files.
//  4. Daemon restart: graceful stop + start, wait for /healthz.
//  5. .gitignore integration: append /.archigraph/ if inside a git repo.
//  6. Persist ~/.archigraph/install.json with install_mode="dev".
//
// Doctor behaviour in DEV mode:
//   - Skips per-file SHA manifest check (files change constantly in dev).
//   - Verifies each ~/.claude/skills/<name> is a symlink with the expected target.
//   - Reports drift when a skill is no longer a symlink (replaced with a copy)
//     or when the symlink target doesn't match dev_target in install.json.
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// DevOptions is the input to RunDev. All fields have sensible defaults;
// callers only need to set overrides.
type DevOptions struct {
	// BinPath is the running archigraph binary.  Defaults to os.Executable().
	BinPath string

	// SkillsSourceDir overrides skills discovery (from --skills-source-dir).
	SkillsSourceDir string

	// ClaudeConfigDirs overrides .claude.json auto-detection.
	ClaudeConfigDirs []string

	// StatePath is the path for install.json.  Defaults to DefaultStatePath().
	StatePath string

	// WorkingDir is used for git repo detection.  Defaults to os.Getwd().
	WorkingDir string

	// Force bypasses the partial-install guard and proceeds even if a
	// previous install left a PartialInstall=true state.
	Force bool

	// DryRun logs every action without writing anything.
	DryRun bool

	// HealthzTimeout is the maximum wait time for the daemon to become healthy
	// after restart.  Defaults to 10 seconds.
	HealthzTimeout time.Duration

	// DaemonPort is the HTTP port where the daemon's /healthz endpoint lives.
	// Defaults to 47274 (the default dashboard port).
	DaemonPort int

	// SkipDaemonRestart skips step 4 (daemon restart + healthz wait).
	// Useful in tests where no real daemon is available.
	SkipDaemonRestart bool

	// RestartDaemon is the injectable daemon restart function. When nil, the
	// production implementation (service.Install + waitForHealthz) is used.
	// Inject a stub in tests to avoid calling launchctl/systemctl.
	RestartDaemon DaemonRestartFunc
}

// DevResult reports what RunDev accomplished.
type DevResult struct {
	// CLIPath and CLISHA256 from step 1.
	CLIPath   string
	CLISHA256 string

	// SkillsLinked lists the skill names that were symlinked in step 2.
	SkillsLinked []string

	// SkillsFallbackCopied lists skill names that fell back to COPY because
	// the symlink/junction failed (Windows locked-down machines only).
	SkillsFallbackCopied []string

	// MCPPaths lists the .claude.json files updated in step 3.
	MCPPaths []string

	// DaemonVersion is the version string returned by /healthz in step 4.
	DaemonVersion string

	// GitignoreRepo is the repo root whose .gitignore was updated in step 5,
	// or empty if we were not inside a git repo.
	GitignoreRepo string

	// StatePath is the path of the written install.json.
	StatePath string
}

// RunDev executes the DEV-mode install transaction.
// It is the implementation behind `archigraph install --dev`.
func RunDev(opts DevOptions) (*DevResult, error) {
	// ── apply defaults ──────────────────────────────────────────────────────
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	// ── guard: refuse to run over a partial/corrupt install ─────────────────
	if !opts.Force {
		if err := guardPartialInstall(opts.StatePath); err != nil {
			return nil, err
		}
	}

	// ── mode-switch warning ─────────────────────────────────────────────────
	// If a previous COPY install exists, warn the user that we are switching
	// modes and that the old COPY skills will be replaced by symlinks.
	if existing, err := ReadState(opts.StatePath); err == nil && existing != nil {
		if existing.InstallMode == ModeCopy {
			fmt.Fprintf(os.Stderr,
				"archigraph install --dev: switching modes; previous COPY skills will be removed and replaced with symlinks\n"+
					"  (run 'archigraph uninstall && archigraph install --dev' to start clean instead)\n")
		}
	}

	result := &DevResult{}
	state := NewState(ModeDev)

	// We track which steps succeeded so rollback is precise.
	var completedSteps []int

	rollback := func(fromStep int) {
		state.PartialInstall = true
		state.RollbackFromStep = fromStep
		for i := len(completedSteps) - 1; i >= 0; i-- {
			s := completedSteps[i]
			switch s {
			case 2:
				rollbackSkillsSymlinks(opts, state)
			case 3:
				rollbackMCPRegistration(CopyOptions{ClaudeConfigDirs: opts.ClaudeConfigDirs}, state)
			}
		}
		if !opts.DryRun {
			_ = WriteState(opts.StatePath, state)
		}
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Step 1: CLI binary identification
	// ─────────────────────────────────────────────────────────────────────────
	clisha, err := sha256File(opts.BinPath)
	if err != nil {
		return nil, fmt.Errorf("step 1 – sha256 binary: %w", err)
	}
	state.CLI = CLIRecord{Path: opts.BinPath, SHA256: clisha}
	result.CLIPath = opts.BinPath
	result.CLISHA256 = clisha
	completedSteps = append(completedSteps, 1)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 2: Skills symlink (dev mode)
	// ─────────────────────────────────────────────────────────────────────────
	skillsDir := skilllink.DiscoverSkillsDir(opts.BinPath, opts.SkillsSourceDir)
	if skillsDir == "" {
		rollback(2)
		return nil, fmt.Errorf("step 2 – no skills/ directory found; set --skills-source-dir to override")
	}

	claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
	if len(claudeDirs) == 0 {
		rollback(2)
		return nil, fmt.Errorf("step 2 – no Claude Code config directories detected")
	}

	primaryClaudeDir := filepath.Dir(claudeDirs[0])
	skillsDestDir := filepath.Join(primaryClaudeDir, "skills")

	skillRecords, linked, fallbackCopied, err := symlinkSkills(skillsDir, skillsDestDir, opts.DryRun)
	if err != nil {
		rollback(2)
		return nil, fmt.Errorf("step 2 – symlink skills: %w", err)
	}
	state.Skills = skillRecords
	result.SkillsLinked = linked
	result.SkillsFallbackCopied = fallbackCopied
	completedSteps = append(completedSteps, 2)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: MCP registration
	// ─────────────────────────────────────────────────────────────────────────
	var registeredPaths []string
	for _, cfgPath := range claudeDirs {
		if opts.DryRun {
			registeredPaths = append(registeredPaths, cfgPath)
			continue
		}
		if _, err := mcpreg.RegisterPath(cfgPath, opts.BinPath); err != nil {
			rollback(3)
			return nil, fmt.Errorf("step 3 – MCP register %s: %w", cfgPath, err)
		}
		registeredPaths = append(registeredPaths, cfgPath)
	}
	state.MCP = MCPRecord{
		Name:            mcpreg.ServerName,
		RegisteredPaths: registeredPaths,
	}
	result.MCPPaths = registeredPaths
	completedSteps = append(completedSteps, 3)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 4: Daemon restart
	// ─────────────────────────────────────────────────────────────────────────
	if !opts.SkipDaemonRestart && !opts.DryRun {
		restartFn := opts.RestartDaemon
		if restartFn == nil {
			restartFn = defaultDaemonRestart
		}
		daemonVersion, err := restartFn(opts.BinPath, opts.DaemonPort, opts.HealthzTimeout)
		if err != nil {
			rollback(4)
			return nil, fmt.Errorf("step 4 – daemon restart: %w", err)
		}
		state.DaemonVersion = daemonVersion
		result.DaemonVersion = daemonVersion
	}
	completedSteps = append(completedSteps, 4)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 5: .gitignore integration
	// ─────────────────────────────────────────────────────────────────────────
	if repoRoot, ok := DetectGitRepo(opts.WorkingDir); ok {
		if !opts.DryRun {
			if _, err := EnsureGitignore(repoRoot); err != nil {
				fmt.Fprintf(os.Stderr, "archigraph install --dev: step 5 warning – .gitignore: %v\n", err)
			} else {
				state.Gitignore = GitignoreRecord{Repos: []string{repoRoot}}
				result.GitignoreRepo = repoRoot
			}
		} else {
			result.GitignoreRepo = repoRoot
		}
	} else {
		fmt.Fprintf(os.Stderr, "archigraph install --dev: step 5 – not inside a git repo; skipping .gitignore update\n")
	}
	completedSteps = append(completedSteps, 5)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: Persist install state
	// ─────────────────────────────────────────────────────────────────────────
	if !opts.DryRun {
		if err := WriteState(opts.StatePath, state); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph install --dev: step 6 warning – persist state: %v\n", err)
		}
	}
	result.StatePath = opts.StatePath
	completedSteps = append(completedSteps, 6)

	return result, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (o *DevOptions) applyDefaults() error {
	if o.BinPath == "" {
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}
		o.BinPath = bin
	}
	if o.StatePath == "" {
		p, err := DefaultStatePath()
		if err != nil {
			return err
		}
		o.StatePath = p
	}
	if o.WorkingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working dir: %w", err)
		}
		o.WorkingDir = cwd
	}
	if o.HealthzTimeout == 0 {
		o.HealthzTimeout = 10 * time.Second
	}
	if o.DaemonPort == 0 {
		o.DaemonPort = 47274
	}
	return nil
}

// symlinkSkills creates a symlink (or directory junction on Windows) from
// destDir/<skillName> → srcDir/<skillName> for every skill in SkillNames.
//
// On Windows, if os.Symlink fails (requires SeCreateSymbolicLinkPrivilege),
// the function falls back to a full directory copy and records the skill name
// in fallbackCopied rather than linked.
//
// Returns:
//   - records: SkillRecord map for install.json (DevTarget set, Files nil)
//   - linked: skill names successfully symlinked
//   - fallbackCopied: skill names that fell back to COPY (Windows only)
func symlinkSkills(srcDir, destDir string, dryRun bool) (map[string]SkillRecord, []string, []string, error) {
	if !dryRun {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return nil, nil, nil, fmt.Errorf("create skills dir %s: %w", destDir, err)
		}
	}

	records := make(map[string]SkillRecord)
	var linked []string
	var fallbackCopied []string

	for _, skillName := range skilllink.SkillNames {
		skillSrc := filepath.Join(srcDir, skillName)
		skillDst := filepath.Join(destDir, skillName)

		if _, err := os.Stat(skillSrc); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph install --dev: skill %s not found at %s; skipping\n", skillName, skillSrc)
			continue
		}

		// Resolve to absolute path for stable symlink target.
		absSrc, err := filepath.Abs(skillSrc)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("skill %s: resolve abs path: %w", skillName, err)
		}

		if !dryRun {
			// Remove existing destination (file, dir, or symlink) to allow
			// idempotent re-runs and mode switching.
			if _, err := os.Lstat(skillDst); err == nil {
				if err := os.RemoveAll(skillDst); err != nil {
					return nil, nil, nil, fmt.Errorf("skill %s: remove existing destination: %w", skillName, err)
				}
			}

			if symlinkErr := os.Symlink(absSrc, skillDst); symlinkErr != nil {
				// On Windows without the symbolic-link privilege, Symlink fails.
				// Fall back to a directory copy and warn.
				fmt.Fprintf(os.Stderr,
					"archigraph install --dev: symlink %s failed (%v); falling back to COPY mode for this skill\n",
					skillName, symlinkErr)
				if copyErr := copyDir(absSrc, skillDst); copyErr != nil {
					return nil, nil, nil, fmt.Errorf("skill %s: copy fallback: %w", skillName, copyErr)
				}
				// Record with Files manifest so doctor's COPY-mode check works.
				srcManifest, merr := buildManifest(absSrc)
				if merr != nil {
					return nil, nil, nil, fmt.Errorf("skill %s: manifest for fallback copy: %w", skillName, merr)
				}
				records[skillName] = SkillRecord{Files: srcManifest}
				fallbackCopied = append(fallbackCopied, skillName)
				continue
			}
		}

		records[skillName] = SkillRecord{DevTarget: absSrc}
		linked = append(linked, skillName)
	}

	return records, linked, fallbackCopied, nil
}

// rollbackSkillsSymlinks removes any skill symlinks or directories that were
// created under destDir during step 2 of a DEV install.
func rollbackSkillsSymlinks(opts DevOptions, state *State) {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
	if len(claudeDirs) == 0 {
		return
	}
	primaryClaudeDir := filepath.Dir(claudeDirs[0])
	skillsDestDir := filepath.Join(primaryClaudeDir, "skills")

	for skillName := range state.Skills {
		dst := filepath.Join(skillsDestDir, skillName)
		_ = os.RemoveAll(dst)
	}
}

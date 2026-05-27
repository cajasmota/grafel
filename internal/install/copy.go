// copy.go implements the COPY mode install transaction for `archigraph install`.
//
// The install proceeds as an atomic 6-step transaction; if any step fails
// the completed steps are rolled back in reverse order (best-effort). The
// state file (~/.archigraph/install.json) is always written to reflect the
// true final state — either a complete install or a partial state with
// RollbackFromStep set.
//
// Steps:
//  1. CLI binary identification (SHA-256 of the running binary).
//  2. Skills copy: copy skills/<name>/ → ~/.claude/skills/<name>/ (no symlinks).
//  3. MCP registration: write archigraph entry into all detected .claude.json files.
//  4. Daemon restart: graceful stop + start, wait for /healthz.
//  5. .gitignore integration: append /.archigraph/ if inside a git repo.
//  6. Persist ~/.archigraph/install.json.
//
// DEV/symlink mode (issue #2212) is NOT implemented here; see TODO below.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/service"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// defaultDaemonRestart is the production daemon restart implementation.
// It calls service.Install (registers/starts the OS service) and then
// polls /healthz to confirm the daemon is up.
func defaultDaemonRestart(binPath string, port int, timeout time.Duration) (string, error) {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return "", fmt.Errorf("resolve daemon layout: %w", err)
	}
	svcOpts := service.Options{
		BinPath:    binPath,
		SocketPath: layout.SocketPath,
		LogDir:     layout.LogDir,
	}
	// Attempt graceful restart: stop existing daemon (if any), then reinstall.
	// service.Install is idempotent — if no service is registered it starts fresh.
	if _, err := service.Install(svcOpts); err != nil {
		return "", fmt.Errorf("service install: %w", err)
	}
	return waitForHealthz(port, timeout)
}

// DaemonRestartFunc is the injectable function for step 4 (daemon restart).
// The default implementation calls service.Install. Tests can inject a no-op
// or a stub that immediately returns a known version string.
//
// Returns the daemon version string reported by /healthz, or an error if
// the daemon could not be restarted or did not become healthy in time.
type DaemonRestartFunc func(binPath string, healthzPort int, timeout time.Duration) (version string, err error)

// CopyOptions is the input to RunCopy. All fields have sensible defaults;
// callers only need to set overrides.
type CopyOptions struct {
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

	// NoHooks skips automatic git hook installation (step 7).
	// Equivalent to passing --no-hooks on the CLI.
	NoHooks bool
}

// CopyResult reports what RunCopy accomplished.
type CopyResult struct {
	// CLIPath and CLISHA256 from step 1.
	CLIPath   string
	CLISHA256 string

	// SkillsInstalled lists the skill names that were copied in step 2.
	SkillsInstalled []string

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

// RunCopy executes the COPY-mode install transaction.
// It is the implementation behind `archigraph install` (no --dev flag).
//
// TODO(#2212): DEV mode (symlinks) is a separate issue; add a RunDev
// wrapper that calls this with a different mode flag when that issue lands.
func RunCopy(opts CopyOptions) (*CopyResult, error) {
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

	result := &CopyResult{}
	state := NewState(ModeCopy)

	// We track which steps succeeded so rollback is precise.
	var completedSteps []int

	rollback := func(fromStep int) {
		state.PartialInstall = true
		state.RollbackFromStep = fromStep
		// Rollback in reverse order of completedSteps.
		for i := len(completedSteps) - 1; i >= 0; i-- {
			s := completedSteps[i]
			switch s {
			case 2:
				rollbackSkillsCopy(opts, state)
			case 3:
				rollbackMCPRegistration(opts, state)
				// Steps 1, 4, 5, 6 are either non-destructive (1, 6) or have
				// best-effort rollback that is a no-op (4 daemon was already
				// running or we just tried to restart it; 5 gitignore append
				// is idempotent and leaving it does not harm anything).
			}
		}
		// Always persist the partial state so doctor / future install
		// can report accurately.
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
	// Step 2: Skills copy
	// ─────────────────────────────────────────────────────────────────────────
	skillsDir := skilllink.DiscoverSkillsDir(opts.BinPath, opts.SkillsSourceDir)
	if skillsDir == "" {
		rollback(2)
		cwd := opts.WorkingDir
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		return nil, fmt.Errorf("no skills/ directory found at %s; pass --skills-source-dir <path-to-archigraph-repo>/skills", cwd)
	}

	claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
	if len(claudeDirs) == 0 {
		rollback(2)
		return nil, fmt.Errorf("step 2 – no Claude Code config directories detected")
	}

	// Copy skills into EVERY detected Claude config dir's skills/ subdir so
	// users running multiple Claude profiles (~/.claude, ~/.claude-personal,
	// etc.) see the skills in all of them.  The SkillRecord/Files manifest
	// is identical regardless of how many destinations receive a copy.
	skillRecords := map[string]SkillRecord{}
	installedSet := map[string]bool{}
	for _, cfgPath := range claudeDirs {
		skillsDestDir := skilllink.ClaudeSkillsDirForConfig(cfgPath)
		if skillsDestDir == "" {
			fmt.Fprintf(os.Stderr,
				"archigraph install: cannot derive skills dir for %s; skipping\n",
				cfgPath)
			continue
		}
		recs, installed, err := copySkills(skillsDir, skillsDestDir, opts.DryRun)
		if err != nil {
			rollback(2)
			return nil, fmt.Errorf("step 2 – copy skills into %s: %w", skillsDestDir, err)
		}
		for name, rec := range recs {
			skillRecords[name] = rec
		}
		for _, name := range installed {
			installedSet[name] = true
		}
	}
	state.Skills = skillRecords
	for _, name := range skilllink.SkillNames {
		if installedSet[name] {
			result.SkillsInstalled = append(result.SkillsInstalled, name)
		}
	}
	completedSteps = append(completedSteps, 2)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 3: MCP registration
	// Register in every detected Claude config dir and in any Windsurf
	// config paths whose parent directory already exists (i.e. Windsurf is
	// installed). Windsurf paths that have no parent dir are silently skipped.
	// ─────────────────────────────────────────────────────────────────────────
	var registeredPaths []string

	// Collect all target paths: Claude dirs first, then Windsurf.
	allMCPTargets := make([]string, len(claudeDirs))
	copy(allMCPTargets, claudeDirs)
	allMCPTargets = append(allMCPTargets, mcpreg.DetectWindsurfPaths()...)

	for _, cfgPath := range allMCPTargets {
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
				// .gitignore failure is non-fatal; warn but continue.
				fmt.Fprintf(os.Stderr, "archigraph install: step 5 warning – .gitignore: %v\n", err)
			} else {
				state.Gitignore = GitignoreRecord{Repos: []string{repoRoot}}
				result.GitignoreRepo = repoRoot
			}
		} else {
			result.GitignoreRepo = repoRoot
		}
	} else {
		fmt.Fprintf(os.Stderr, "archigraph install: step 5 – not inside a git repo; skipping .gitignore update\n")
	}
	completedSteps = append(completedSteps, 5)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 6: Persist install state
	// ─────────────────────────────────────────────────────────────────────────
	if !opts.DryRun {
		if err := WriteState(opts.StatePath, state); err != nil {
			// This is non-fatal for the install but important for future doctor
			// runs, so warn loudly.
			fmt.Fprintf(os.Stderr, "archigraph install: step 6 warning – persist state: %v\n", err)
		}
	}
	result.StatePath = opts.StatePath
	completedSteps = append(completedSteps, 6)

	// ─────────────────────────────────────────────────────────────────────────
	// Step 7: Git hook installation (post-checkout, post-merge, post-rewrite,
	//         pre-push). Non-fatal: a hook install failure warns but does not
	//         roll back the rest of the install.  Can be opted-out with
	//         --no-hooks.
	// ─────────────────────────────────────────────────────────────────────────
	if !opts.NoHooks {
		hookOpts := HookInstallOptions{
			RepoPath: opts.WorkingDir,
			DryRun:   opts.DryRun,
		}
		if err := InstallGitHooks(hookOpts); err != nil {
			// Non-fatal: hooks are a convenience; don't abort a successful install.
			fmt.Fprintf(os.Stderr, "archigraph install: step 7 warning – git hooks: %v\n", err)
		}
	}

	return result, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (o *CopyOptions) applyDefaults() error {
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

// guardPartialInstall returns an error if install.json exists and
// describes a partial/corrupt install. This prevents users from running
// `archigraph install` twice and ending up with half-applied state.
func guardPartialInstall(statePath string) error {
	st, err := ReadState(statePath)
	if err != nil {
		// Unreadable state file — treat as corrupt; require --force.
		return fmt.Errorf(
			"install.json at %s is unreadable (%v); run `archigraph install --force` or `archigraph uninstall && archigraph install` to recover",
			statePath, err)
	}
	if st == nil {
		// No prior install — proceed normally.
		return nil
	}
	if st.PartialInstall {
		return fmt.Errorf(
			"a partial install was detected (rolled back from step %d); run `archigraph install --force` to retry or `archigraph uninstall && archigraph install` to start clean",
			st.RollbackFromStep)
	}
	return nil
}

// sha256File computes the hex-encoded SHA-256 hash of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sha256Bytes returns the hex-encoded SHA-256 hash of b.
func sha256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// copySkills copies every skill from srcDir/<skillName>/ to destDir/<skillName>/.
// It is idempotent: if the destination already matches the source (same per-file SHAs),
// the copy is skipped for that skill.
// Returns the SkillRecord map (for install.json) and the list of skill names processed.
func copySkills(srcDir, destDir string, dryRun bool) (map[string]SkillRecord, []string, error) {
	if !dryRun {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create skills dir %s: %w", destDir, err)
		}
		// Prune orphan symlinks left behind by an earlier DEV install (or a
		// previously-shipped skill that has since been renamed/retired).  We
		// only remove symlinks here; regular directories are left intact in
		// case a user installed a skill manually.
		skilllink.PruneOrphanSkillSymlinks(os.Stderr, destDir)
	}

	records := make(map[string]SkillRecord)
	var installed []string

	for _, skillName := range skilllink.SkillNames {
		skillSrc := filepath.Join(srcDir, skillName)
		skillDst := filepath.Join(destDir, skillName)

		if _, err := os.Stat(skillSrc); err != nil {
			// Source skill dir missing — skip with warning rather than fail the
			// entire install. This allows partial skill sets in development.
			fmt.Fprintf(os.Stderr, "archigraph install: skill %s not found at %s; skipping\n", skillName, skillSrc)
			continue
		}

		// Build the per-file SHA manifest from the source.
		srcManifest, err := buildManifest(skillSrc)
		if err != nil {
			return nil, nil, fmt.Errorf("skill %s manifest: %w", skillName, err)
		}

		// Check whether destination already matches (idempotency).
		if !dryRun {
			dstManifest, _ := buildManifest(skillDst) // ignore error — dest may not exist
			if manifestsEqual(srcManifest, dstManifest) {
				// Already up to date — record and skip copy.
				records[skillName] = SkillRecord{Files: srcManifest}
				installed = append(installed, skillName)
				continue
			}

			// Destination exists but differs — remove and re-copy.
			_ = os.RemoveAll(skillDst)

			if err := copyDir(skillSrc, skillDst); err != nil {
				return nil, nil, fmt.Errorf("copy skill %s: %w", skillName, err)
			}
		}

		records[skillName] = SkillRecord{Files: srcManifest}
		installed = append(installed, skillName)
	}

	return records, installed, nil
}

// buildManifest returns a map of relative-file-path → hex SHA-256 for every
// regular file under root.
func buildManifest(root string) (map[string]string, error) {
	manifest := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		// Normalise to forward slashes in the manifest so the JSON is
		// platform-agnostic and doctor can compare across OS boundaries.
		rel = filepath.ToSlash(rel)
		manifest[rel] = sha256Bytes(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// manifestsEqual returns true when a and b have identical keys and values.
func manifestsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// copyDir recursively copies the directory tree at src into dst.
// dst is created if it does not exist.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies the single file at src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// waitForHealthz polls http://127.0.0.1:<port>/healthz until it returns a
// 200 or the timeout elapses. Returns the response body (daemon version string)
// on success.
func waitForHealthz(port int, timeout time.Duration) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	deadline := time.Now().Add(timeout)
	const pollInterval = 300 * time.Millisecond

	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return string(body), nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(pollInterval)
	}
	return "", fmt.Errorf("daemon did not respond on %s within %s; run 'archigraph start' and retry", url, timeout)
}

// ── rollback helpers ──────────────────────────────────────────────────────────

// rollbackSkillsCopy removes any skill directories that were copied to
// every detected Claude config dir during step 2.
func rollbackSkillsCopy(opts CopyOptions, state *State) {
	claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
	if len(claudeDirs) == 0 {
		return
	}
	for _, cfgPath := range claudeDirs {
		skillsDestDir := skilllink.ClaudeSkillsDirForConfig(cfgPath)
		if skillsDestDir == "" {
			continue
		}
		for skillName := range state.Skills {
			dst := filepath.Join(skillsDestDir, skillName)
			_ = os.RemoveAll(dst)
		}
	}
}

// rollbackMCPRegistration removes the archigraph entry from any .claude.json
// files that were registered during step 3.
func rollbackMCPRegistration(_ CopyOptions, state *State) {
	for _, cfgPath := range state.MCP.RegisteredPaths {
		_ = mcpreg.UnregisterPath(cfgPath)
	}
}

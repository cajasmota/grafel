// Package install — doctor.go
//
// RunDoctor and its helpers implement `grafel doctor` (#2211).
//
// Doctor reads ~/.grafel/install.json as ground truth and compares it
// against live state across five surfaces:
//
//   - CLI binary SHA-256 (Critical)
//   - Daemon /healthz reachability + version (Critical)
//   - Skills per-file SHA manifests (Critical)
//   - MCP registration in detected .claude.json files (Critical)
//   - Conventions per-file SHA manifests (Warning)
//   - .gitignore contains /.grafel/ in tracked repos (Warning)
//   - Stale staging directories older than 7 days (Info)
//
// JSON schema is pinned at schema_version=1.  Bump SchemaVersion when
// the shape of DoctorReport changes in a backward-incompatible way.
package install

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/service"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/install/skilllink"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// DoctorSchemaVersion is the JSON schema version for DoctorReport.
// Increment this (and handle old versions in readers) if the shape
// of DoctorReport changes in a backward-incompatible way.
const DoctorSchemaVersion = 1

// Severity of a check result.
type Severity string

const (
	SeverityCritical Severity = "critical" // exit non-zero
	SeverityWarning  Severity = "warning"  // print but exit zero
	SeverityInfo     Severity = "info"     // advisory only
)

// CheckResult is the result of a single doctor check.
// Every surface produces exactly one CheckResult.
type CheckResult struct {
	// Surface is the human-readable name of what was checked.
	// Examples: "cli", "daemon", "skills/generate-docs", "mcp", "gitignore".
	Surface string `json:"surface"`

	// OK is true when the check passed with no drift.
	OK bool `json:"ok"`

	// Severity is the severity of the check (critical / warning / info).
	// Only meaningful when OK is false.
	Severity Severity `json:"severity,omitempty"`

	// Drift is the list of specific drift descriptions.
	// Empty when OK is true.
	Drift []string `json:"drift,omitempty"`
}

// DoctorReport is the top-level struct written by --json.
// Schema is pinned at schema_version=1; see DoctorSchemaVersion.
type DoctorReport struct {
	// SchemaVersion is always DoctorSchemaVersion (currently 1).
	SchemaVersion int `json:"schema_version"`

	// OK is true when all Critical checks passed (warnings/info do not affect this).
	OK bool `json:"ok"`

	// Checks is the ordered list of per-surface results.
	Checks []CheckResult `json:"checks"`

	// Remediation is a human-readable suggested fix, set when OK is false.
	Remediation string `json:"remediation,omitempty"`
}

// DoctorOptions controls RunDoctor behaviour.
type DoctorOptions struct {
	// StatePath is the path of install.json.  Defaults to DefaultStatePath().
	StatePath string

	// ClaudeConfigDirs overrides .claude.json auto-detection (same flag as install).
	ClaudeConfigDirs []string

	// DaemonPort is the HTTP port for the daemon's /healthz endpoint.
	// Defaults to 47274.
	DaemonPort int

	// DaemonTimeout is the maximum wait for the /healthz call.
	// Defaults to 2 seconds.
	DaemonTimeout time.Duration

	// SkillsDir is the primary Claude skills directory.
	// When empty it is derived from ClaudeConfigDirs or auto-detected from HOME.
	SkillsDir string

	// groupsFn / loadGroupFn resolve the registered groups and their configs
	// for the per-enabled-tool checks (#5258). When nil they default to
	// registry.Groups / registry.LoadGroupConfig. Injectable so tests can
	// drive the enabled-tool set without a real registry on disk.
	groupsFn     func() ([]registry.GroupRef, error)
	loadGroupFn  func(path string) (*registry.GroupConfig, error)
	mcpEntryFn   func(cfgPath string) (missing bool, drift string)
	mcpPathForFn func(tool mcpreg.Tool) (string, error)
}

func (o *DoctorOptions) applyDefaults() error {
	if o.StatePath == "" {
		p, err := DefaultStatePath()
		if err != nil {
			return err
		}
		o.StatePath = p
	}
	if o.DaemonPort == 0 {
		o.DaemonPort = 47274
	}
	if o.DaemonTimeout == 0 {
		o.DaemonTimeout = 2 * time.Second
	}
	return nil
}

// RunDoctor performs all doctor checks and returns the report.
// It never returns an error for check failures — failures are expressed
// inside DoctorReport.  An error is only returned for programming
// mistakes (e.g. invalid opts).
func RunDoctor(opts DoctorOptions) (*DoctorReport, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	report := &DoctorReport{
		SchemaVersion: DoctorSchemaVersion,
		OK:            true,
	}

	state, err := ReadState(opts.StatePath)
	if err != nil {
		// install.json unreadable — single critical failure
		report.OK = false
		report.Checks = []CheckResult{{
			Surface:  "install.json",
			OK:       false,
			Severity: SeverityCritical,
			Drift:    []string{fmt.Sprintf("cannot read install.json at %s: %v", opts.StatePath, err)},
		}}
		report.Remediation = "Run: grafel install"
		return report, nil
	}

	if state == nil {
		report.OK = false
		report.Checks = []CheckResult{{
			Surface:  "install.json",
			OK:       false,
			Severity: SeverityCritical,
			Drift:    []string{fmt.Sprintf("install.json not found at %s — grafel has not been installed", opts.StatePath)},
		}}
		report.Remediation = "Run: grafel install"
		return report, nil
	}

	// Derive skills dir from ClaudeConfigDirs or auto-detection.
	skillsDir := opts.SkillsDir
	if skillsDir == "" {
		claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
		if len(claudeDirs) > 0 {
			skillsDir = skilllink.ClaudeSkillsDirForConfig(claudeDirs[0])
		}
	}

	// ── Check 1: CLI binary SHA ─────────────────────────────────────────────
	report.Checks = append(report.Checks, checkCLI(state))

	// ── Check 2: Daemon /healthz ────────────────────────────────────────────
	report.Checks = append(report.Checks, checkDaemon(state, opts.DaemonPort, opts.DaemonTimeout))

	// ── Check 2b: Engine liveness + version skew (ADR-0024 PR5, epic #5729) ──
	// Monolith-aware: in the DEFAULT config (GRAFEL_SPLIT_MODE off) there is no
	// separate engine process, so this must never warn "engine down" — see
	// checkEngineLiveness's doc comment for the monolith/split detection logic.
	report.Checks = append(report.Checks, checkEngineLiveness(state, defaultEngineLivenessDeps()))

	// ── Check 2c: Pre-split OS service unit (ADR-0024 PR5, epic #5729) ───────
	// Purely informational: an existing install's unit may still literally
	// exec `grafel daemon` until the next `grafel update`/`grafel install`
	// re-renders it to `grafel serve` (behavior-identical while the split flag
	// is off — see service.Install's WriteUnit re-render contract).
	if preSplit := checkPreSplitUnit(); preSplit != nil {
		report.Checks = append(report.Checks, *preSplit)
	}

	// ── Check 3: Skills per-file SHA manifests (COPY) or symlink targets (DEV) ─
	for skillName, skillRecord := range state.Skills {
		if state.InstallMode == ModeDev {
			report.Checks = append(report.Checks, checkSkillDev(skillName, skillRecord, skillsDir))
		} else {
			report.Checks = append(report.Checks, checkSkill(skillName, skillRecord, skillsDir))
		}
	}

	// ── Check 4: MCP registration ───────────────────────────────────────────
	claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
	report.Checks = append(report.Checks, checkMCP(state, claudeDirs))

	// ── Check 5: .gitignore in tracked repos ────────────────────────────────
	for _, repo := range state.Gitignore.Repos {
		report.Checks = append(report.Checks, checkGitignore(repo))
	}

	// ── Check 6: Per-repo IDE rules files (issue #2683) ─────────────────────
	for _, c := range checkRulesFiles() {
		report.Checks = append(report.Checks, c)
	}

	// ── Check 7: Per-enabled-tool wiring (issue #5258) ──────────────────────
	// For every tool ENABLED in any group's config, verify its own artifacts:
	// the MCP entry in that tool's config file (where SupportsMCP), and its
	// rules file(s) across the group's repos. Skills are Claude-only and are
	// already covered by Check 3, so they are not re-reported here. These rows
	// are Warning severity (a per-tool gap doesn't break the install) — Claude's
	// critical MCP/skills checks above keep their existing severity.
	for _, c := range checkEnabledTools(opts) {
		report.Checks = append(report.Checks, c)
	}

	// ── Check 8: Stale staging dirs ─────────────────────────────────────────
	if staleCheck := checkStaleStagingDirs(opts.StatePath); staleCheck != nil {
		report.Checks = append(report.Checks, *staleCheck)
	}

	// Determine overall OK: all Critical checks must pass.
	hasCriticalFailure := false
	for _, c := range report.Checks {
		if !c.OK && c.Severity == SeverityCritical {
			hasCriticalFailure = true
			break
		}
	}
	report.OK = !hasCriticalFailure
	if !report.OK {
		report.Remediation = "Run: grafel install"
	}

	return report, nil
}

// checkCLI compares the running binary SHA against install.json.
// Skips the check if running from a git worktree (which may have a different
// binary path), and emits an INFO-level hint instead.
func checkCLI(state *State) CheckResult {
	cr := CheckResult{Surface: "cli", OK: true}

	if state.CLI.Path == "" || state.CLI.SHA256 == "" {
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{"install.json has no CLI record (partial install?)"}
		return cr
	}

	// Check if we're running from a git worktree. If so, skip the SHA check
	// since worktree builds may have a different binary layout.
	// Detect by checking if .git is a file (worktree marker) in the current dir
	// or any parent directory. This is a cheap heuristic.
	if isInGitWorktree() {
		cr.OK = true
		cr.Severity = SeverityInfo
		cr.Drift = []string{"running from git worktree; binary-SHA check skipped. To update: run 'go install ./cmd/grafel' from the repo root."}
		return cr
	}

	// Compute current SHA.
	actual, err := sha256File(state.CLI.Path)
	if err != nil {
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{fmt.Sprintf("cannot hash binary %s: %v", state.CLI.Path, err)}
		return cr
	}

	if actual != state.CLI.SHA256 {
		// #4463: a SHA drift alone is NOT a broken install — the daemon stays
		// fully usable (status.go already treats this as a non-blocking note).
		// It typically means the binary was rebuilt/upgraded in place since the
		// last `install`. Surface it as a Warning (exit-zero advisory), reserving
		// Critical for unreadable/missing install.json and unhashable binaries.
		cr.OK = false
		cr.Severity = SeverityWarning
		cr.Drift = []string{fmt.Sprintf("sha256 mismatch: binary=%s install=%s (daemon still usable; re-run 'grafel install' to refresh)", actual[:16], state.CLI.SHA256[:16])}
	}
	return cr
}

// isInGitWorktree reports whether the current process is running from inside
// a git worktree (not the main checkout). A worktree has .git as a file
// (not a directory) pointing to the .git/worktrees/<name> directory.
func isInGitWorktree() bool {
	return isGitDirAFile(".")
}

// isGitDirAFile checks if .git in dir is a regular file (worktree marker).
func isGitDirAFile(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	// In a worktree, .git is a file. In a normal checkout, it's a directory.
	return !info.IsDir()
}

// healthzResponse is a minimal struct for parsing the /healthz JSON body.
type healthzResponse struct {
	Version string `json:"version"`
}

// checkDaemon probes /healthz and validates the version against install.json.
func checkDaemon(state *State, port int, timeout time.Duration) CheckResult {
	cr := CheckResult{Surface: "daemon", OK: true}

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{fmt.Sprintf("build request: %v", err)}
		return cr
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{fmt.Sprintf("daemon unreachable at %s: %v", url, err)}
		return cr
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{fmt.Sprintf("daemon /healthz returned HTTP %d", resp.StatusCode)}
		return cr
	}

	body, _ := io.ReadAll(resp.Body)
	// Try to parse JSON version response; also accept plain text version.
	var hzResp healthzResponse
	if jerr := json.Unmarshal(body, &hzResp); jerr != nil {
		// Plain text fallback.
		hzResp.Version = strings.TrimSpace(string(body))
	}

	if hzResp.Version == "" {
		cr.Drift = append(cr.Drift, "daemon /healthz returned no version")
		// Warning only — daemon is up but didn't report version.
		cr.OK = false
		cr.Severity = SeverityWarning
		return cr
	}

	// If install.json recorded a daemon version, compare.
	if state.DaemonVersion != "" && hzResp.Version != state.DaemonVersion {
		cr.OK = false
		cr.Severity = SeverityWarning
		cr.Drift = []string{fmt.Sprintf("daemon version mismatch: running=%s installed=%s", hzResp.Version, state.DaemonVersion)}
	}

	return cr
}

// checkSkill compares the installed skill's files against the SHA manifest
// recorded in install.json.
func checkSkill(skillName string, record SkillRecord, skillsDir string) CheckResult {
	cr := CheckResult{Surface: "skills/" + skillName, OK: true, Severity: SeverityCritical}

	if skillsDir == "" {
		cr.OK = false
		cr.Drift = []string{"skills directory not determined (install.json has no MCP registered_paths?)"}
		return cr
	}

	skillDir := filepath.Join(skillsDir, skillName)
	if _, err := os.Stat(skillDir); err != nil {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("skill directory missing: %s", skillDir)}
		return cr
	}

	// Build live manifest.
	liveManifest, err := buildManifest(skillDir)
	if err != nil {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("cannot build manifest for %s: %v", skillDir, err)}
		return cr
	}

	// Compare against install.json manifest.
	for relPath, installedSHA := range record.Files {
		liveSHA, ok := liveManifest[relPath]
		if !ok {
			cr.OK = false
			cr.Drift = append(cr.Drift, fmt.Sprintf("%s missing", relPath))
			continue
		}
		if liveSHA != installedSHA {
			cr.OK = false
			cr.Drift = append(cr.Drift, fmt.Sprintf("%s sha mismatch", relPath))
		}
	}

	// Check for extra files that weren't in the install manifest (not an error,
	// but might indicate manual edits — we just silently ignore extras for now).

	if len(cr.Drift) > 0 {
		cr.OK = false
		cr.Severity = SeverityCritical
	}
	return cr
}

// checkSkillDev verifies that a DEV-mode skill is correctly symlinked:
//  1. The destination path exists.
//  2. It is a symlink (not a regular directory — that would indicate drift).
//  3. The symlink's resolved target matches the DevTarget recorded in install.json.
func checkSkillDev(skillName string, record SkillRecord, skillsDir string) CheckResult {
	cr := CheckResult{Surface: "skills/" + skillName, OK: true, Severity: SeverityCritical}

	if skillsDir == "" {
		cr.OK = false
		cr.Drift = []string{"skills directory not determined (install.json has no MCP registered_paths?)"}
		return cr
	}

	skillDst := filepath.Join(skillsDir, skillName)

	// Use Lstat to see the symlink itself, not what it points to.
	info, err := os.Lstat(skillDst)
	if err != nil {
		if os.IsNotExist(err) {
			cr.OK = false
			cr.Drift = []string{fmt.Sprintf("skill symlink missing: %s", skillDst)}
		} else {
			cr.OK = false
			cr.Drift = []string{fmt.Sprintf("cannot stat skill destination %s: %v", skillDst, err)}
		}
		return cr
	}

	// If the record has a fallback copy (Files set, no DevTarget), use COPY-mode check.
	if record.DevTarget == "" && len(record.Files) > 0 {
		return checkSkill(skillName, record, skillsDir)
	}

	// Must be a symlink.
	if info.Mode()&os.ModeSymlink == 0 {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("%s is not a symlink (replaced with a copy?); run `grafel install --dev --force` to restore", skillDst)}
		return cr
	}

	// Resolve the symlink target.
	target, err := os.Readlink(skillDst)
	if err != nil {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("cannot read symlink %s: %v", skillDst, err)}
		return cr
	}

	// Compare against recorded DevTarget. Both should be absolute paths; if
	// the target is relative, resolve it relative to the symlink's directory.
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(skillDst), target)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("cannot resolve symlink target %s: %v", target, err)}
		return cr
	}

	absExpected, err := filepath.Abs(record.DevTarget)
	if err != nil {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("cannot resolve expected dev_target %s: %v", record.DevTarget, err)}
		return cr
	}

	if absTarget != absExpected {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf(
			"symlink target mismatch: link points to %s, install.json says %s",
			absTarget, absExpected,
		)}
	}

	return cr
}

// checkMCP verifies that the grafel MCP entry is present in every
// registered .claude.json path.
func checkMCP(state *State, claudeDirs []string) CheckResult {
	cr := CheckResult{Surface: "mcp", OK: true, Severity: SeverityCritical}

	if state.MCP.Name == "" && len(state.MCP.RegisteredPaths) == 0 {
		// MCP was never registered (e.g. step 3 was not reached or partial install).
		cr.OK = false
		cr.Drift = []string{"MCP registration not recorded in install.json"}
		return cr
	}

	// Check each registered path still has the entry.
	for _, cfgPath := range state.MCP.RegisteredPaths {
		if missing, drift := mcpEntryDrift(cfgPath); missing || drift != "" {
			cr.OK = false
			d := cfgPath
			if drift != "" {
				d += ": " + drift
			}
			cr.Drift = append(cr.Drift, d)
		}
	}

	// Also check auto-detected paths that weren't in install.json.
	recordedSet := make(map[string]bool, len(state.MCP.RegisteredPaths))
	for _, p := range state.MCP.RegisteredPaths {
		recordedSet[p] = true
	}
	for _, cfgPath := range claudeDirs {
		if recordedSet[cfgPath] {
			continue
		}
		// Not in install record — check if it has an entry anyway.
		missing, _ := mcpEntryDrift(cfgPath)
		if missing {
			// It's in auto-detected dirs but not registered — warn.
			cr.OK = false
			cr.Severity = SeverityWarning
			cr.Drift = append(cr.Drift, fmt.Sprintf("%s: grafel entry absent (not in install record)", cfgPath))
		}
	}

	if len(cr.Drift) > 0 && cr.Severity == SeverityCritical {
		cr.OK = false
	}
	return cr
}

// mcpEntryDrift returns (missing=true, "") when the grafel entry is absent,
// or (false, drift) when the entry is present but has changed.
func mcpEntryDrift(cfgPath string) (missing bool, drift string) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return true, fmt.Sprintf("cannot read %s: %v", cfgPath, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, fmt.Sprintf("invalid JSON in %s: %v", cfgPath, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		return true, ""
	}
	_, ok := servers[mcpreg.ServerName]
	if !ok {
		return true, ""
	}
	return false, ""
}

// checkGitignore verifies that the .gitignore in repoRoot contains /.grafel/.
func checkGitignore(repoRoot string) CheckResult {
	cr := CheckResult{Surface: "gitignore/" + filepath.Base(repoRoot), OK: true, Severity: SeverityWarning}

	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			cr.OK = false
			cr.Drift = []string{".gitignore not found"}
			return cr
		}
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("cannot read .gitignore: %v", err)}
		return cr
	}

	if !hasGitignoreEntry(data, grafelGitignoreEntry) {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("/.grafel/ missing from %s", gitignorePath)}
	}
	return cr
}

// checkRulesFiles scans every registered repo (across every grafel
// group) for the per-IDE rules files defined in the rulesfiles package
// (AGENTS.md, CLAUDE.md, .windsurfrules, .cursorrules,
// .codeium/instructions.md, .github/copilot-instructions.md).
//
// Per repo, one CheckResult is emitted. The check is OK when every
// target file contains the current grafel block; otherwise it lists
// each non-OK file by status (MISSING/STALE/OUTDATED). Severity is
// Warning — a missing rules block doesn't break grafel, but it does
// mean the corresponding IDE agent won't reach for the MCP first.
//
// Failures of the registry read are returned as a single Info-severity
// check so a fresh machine (no groups yet) doesn't appear "broken".
func checkRulesFiles() []CheckResult {
	groups, err := registry.Groups()
	if err != nil {
		return []CheckResult{{
			Surface:  "rules-files",
			OK:       false,
			Severity: SeverityInfo,
			Drift:    []string{fmt.Sprintf("cannot read registry: %v", err)},
		}}
	}
	if len(groups) == 0 {
		// No groups registered — nothing to scan. Don't emit a check.
		return nil
	}

	var results []CheckResult
	for _, g := range groups {
		cfg, lerr := registry.LoadGroupConfig(g.ConfigPath)
		if lerr != nil || cfg == nil {
			results = append(results, CheckResult{
				Surface:  "rules-files/" + g.Name,
				OK:       false,
				Severity: SeverityWarning,
				Drift:    []string{fmt.Sprintf("cannot load group config %s: %v", g.ConfigPath, lerr)},
			})
			continue
		}
		for _, repo := range cfg.Repos {
			results = append(results, scanRulesFilesForRepo(g.Name, repo.Path))
		}
	}
	return results
}

// scanRulesFilesForRepo runs rulesfiles.Scan on a single repo and
// converts the result into a CheckResult.
func scanRulesFilesForRepo(group, repoPath string) CheckResult {
	cr := CheckResult{
		Surface:  fmt.Sprintf("rules-files/%s/%s", group, filepath.Base(repoPath)),
		OK:       true,
		Severity: SeverityWarning,
	}
	statuses := rulesfiles.Scan(repoPath)
	for _, st := range statuses {
		if st.Status == rulesfiles.StatusOK {
			continue
		}
		cr.OK = false
		label := strings.ToUpper(string(st.Status))
		if st.Detail != "" {
			cr.Drift = append(cr.Drift, fmt.Sprintf("%s [%s] — %s", st.Target, label, st.Detail))
		} else {
			cr.Drift = append(cr.Drift, fmt.Sprintf("%s [%s]", st.Target, label))
		}
	}
	return cr
}

// checkEnabledTools emits one CheckResult per (group, enabled-tool) pair,
// validating that tool's own wiring (issue #5258):
//
//   - MCP: when the adapter SupportsMCP, the grafel entry must be present in
//     that tool's config file (mcpreg.SettingsPath — .claude.json, Cursor's
//     ~/.cursor/mcp.json, Windsurf's mcp_config.json, Codex's config.toml,
//     Kiro's mcp.json). A missing config file is reported as "mcp not wired"
//     rather than an error — the tool may simply not be installed.
//   - rules: each of the adapter's rules-file targets must contain the current
//     grafel block in every repo of the group.
//
// Severity is Warning: a per-tool gap means that tool's agent won't reach for
// the grafel MCP first, but it does not break the install. Claude's critical
// MCP/skills checks (Checks 3 & 4) are unchanged. When no groups are
// registered the slice is empty (a fresh machine isn't "broken").
func checkEnabledTools(opts DoctorOptions) []CheckResult {
	bindings := resolveEnabledToolBindings(opts.groupsFn, opts.loadGroupFn)
	if len(bindings) == 0 {
		return nil
	}

	mcpEntry := opts.mcpEntryFn
	if mcpEntry == nil {
		mcpEntry = mcpEntryDrift
	}
	mcpPathFor := opts.mcpPathForFn
	if mcpPathFor == nil {
		mcpPathFor = mcpreg.SettingsPath
	}

	var results []CheckResult
	for _, b := range bindings {
		a := b.adapter
		cr := CheckResult{
			Surface:  fmt.Sprintf("tool/%s/%s", b.group, a.ID()),
			OK:       true,
			Severity: SeverityWarning,
		}

		// ── MCP entry in the tool's own config file ───────────────────────
		if a.SupportsMCP() {
			tool := a.MCPTool()
			cfgPath, err := mcpPathFor(tool)
			if err != nil {
				cr.OK = false
				cr.Drift = append(cr.Drift, fmt.Sprintf("mcp: cannot resolve config path: %v", err))
			} else if _, statErr := os.Stat(cfgPath); statErr != nil {
				// Tool's config file absent — tool likely not installed.
				cr.OK = false
				cr.Drift = append(cr.Drift, fmt.Sprintf("mcp not wired (%s absent — tool installed?)", cfgPath))
			} else if missing, drift := mcpEntry(cfgPath); missing {
				cr.OK = false
				cr.Drift = append(cr.Drift, fmt.Sprintf("mcp entry absent from %s", cfgPath))
			} else if drift != "" {
				cr.OK = false
				cr.Drift = append(cr.Drift, fmt.Sprintf("mcp: %s", drift))
			}
		}

		// ── rules file(s) across the group's repos ────────────────────────
		want := map[string]bool{}
		for _, t := range a.RulesFileTargets() {
			want[t] = true
		}
		if len(want) > 0 {
			for _, repo := range b.repos {
				for _, st := range rulesfiles.Scan(repo) {
					if !want[st.Target] || st.Status == rulesfiles.StatusOK {
						continue
					}
					cr.OK = false
					label := strings.ToUpper(string(st.Status))
					cr.Drift = append(cr.Drift, fmt.Sprintf("rules %s [%s] (%s)", st.Target, label, filepath.Base(repo)))
				}
			}
		}

		results = append(results, cr)
	}
	return results
}

// checkStaleStagingDirs looks for .grafel/staging/<run_id>/ directories
// older than 7 days under the grafel state root.
// Returns nil when no stale dirs exist (avoids adding a check with no content).
func checkStaleStagingDirs(statePath string) *CheckResult {
	grafelDir := filepath.Dir(statePath)
	stagingDir := filepath.Join(grafelDir, "staging")

	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		// staging dir doesn't exist — nothing to report
		return nil
	}

	threshold := time.Now().Add(-7 * 24 * time.Hour)
	var staleDirs []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			staleDirs = append(staleDirs, filepath.Join(stagingDir, e.Name()))
		}
	}

	if len(staleDirs) == 0 {
		return nil
	}

	drift := make([]string, 0, len(staleDirs))
	for _, d := range staleDirs {
		drift = append(drift, fmt.Sprintf("%s (older than 7 days)", d))
	}

	cr := CheckResult{
		Surface:  "staging",
		OK:       false,
		Severity: SeverityInfo,
		Drift:    drift,
	}
	return &cr
}

// ── Engine liveness + version skew (ADR-0024 PR5, epic #5729) ─────────────────

// engineLivenessDeps abstracts the I/O checkEngineLiveness needs so tests can
// drive monolith-mode / split-mode / fresh-heartbeat / stale-heartbeat /
// version-skew scenarios without a real daemon root, a real engine.pid, or a
// real statusfile on disk.
type engineLivenessDeps struct {
	// root resolves the daemon root directory. Mirrors daemon.DefaultLayout.
	root func() (string, error)
	// readEnginePID reads and parses engine.pid at the given path. Any error
	// (including os.IsNotExist — the common monolith-mode case) means "no
	// engine.pid": split mode is off, or the engine already exited and was
	// reaped. Mirrors internal/daemon/service.defaultReadEnginePID.
	readEnginePID func(path string) (int, error)
	// readLiveness reads the engine-global liveness statusfile at key.
	// Mirrors statusfile.Read(daemon.EngineLivenessStatusKey(root)).
	readLiveness func(key string) (*statusfile.File, error)
	// staleAfter returns the max heartbeat age before it's stale. Mirrors
	// daemon.EngineHeartbeatStaleAfter — the SAME threshold the serve-side
	// supervisor's own health gate uses.
	staleAfter func() time.Duration
}

// defaultEngineLivenessDeps wires engineLivenessDeps to the real daemon /
// statusfile packages.
func defaultEngineLivenessDeps() engineLivenessDeps {
	return engineLivenessDeps{
		root: func() (string, error) {
			layout, err := daemon.DefaultLayout()
			if err != nil {
				return "", err
			}
			return layout.Root, nil
		},
		readEnginePID: func(path string) (int, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return 0, err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return 0, err
			}
			return pid, nil
		},
		readLiveness: statusfile.Read,
		staleAfter:   daemon.EngineHeartbeatStaleAfter,
	}
}

// checkEngineLiveness reports the health of the `grafel engine` child
// process — but ONLY when the serve/engine split is actually active.
//
// ADR-0024's split is opt-in via GRAFEL_SPLIT_MODE, default OFF. In that
// default (monolith) configuration there is NO separate engine process at
// all — serve does all indexing in-process, exactly like the pre-split
// `grafel daemon`. Mode detection here does NOT read SplitModeEnabled()
// directly (that reflects THIS process's environment, not necessarily the
// installed service's); instead it uses the observable artifact split mode
// produces: an engine.pid file. No engine.pid means either split mode is off,
// or a split-mode engine child already exited and was reaped (serve's own
// graceful drain, or service.Uninstall's orphan sweep) — either way there is
// no live engine to be "down", so this reports healthy/in-process rather than
// warning.
//
// Only when engine.pid is present (split mode is/was active) do the
// liveness-freshness and version-skew checks apply, mirroring the exact
// staleness definition and pid-matching the serve-side supervisor's own
// health gate uses (see internal/daemon/supervise.go's engineSupervisor.healthy).
func checkEngineLiveness(state *State, deps engineLivenessDeps) CheckResult {
	cr := CheckResult{Surface: "engine", OK: true}

	root, err := deps.root()
	if err != nil {
		// Can't resolve a daemon root at all — not an engine-liveness
		// problem per se (checkDaemon already covers daemon reachability);
		// report info rather than a false "engine down".
		cr.Severity = SeverityInfo
		cr.Drift = []string{fmt.Sprintf("cannot resolve daemon root: %v", err)}
		return cr
	}

	pidPath := daemon.EnginePIDPath(root)
	pid, err := deps.readEnginePID(pidPath)
	if err != nil || pid <= 0 {
		// No engine.pid — monolith mode (the default), or the engine child
		// already exited and was reaped. There is no separate engine process
		// to be down. Healthy, in-process.
		cr.Drift = []string{"monolith mode: no separate engine process (GRAFEL_SPLIT_MODE off)"}
		cr.Severity = SeverityInfo
		return cr
	}

	// engine.pid exists — split mode is (or was) active. Validate the
	// engine-global liveness heartbeat the same way serve's own supervisor
	// does.
	f, ferr := deps.readLiveness(daemon.EngineLivenessStatusKey(root))
	if ferr != nil {
		cr.OK = false
		cr.Severity = SeverityWarning
		cr.Drift = []string{fmt.Sprintf("engine degraded: liveness statusfile unreadable: %v", ferr)}
		return cr
	}

	if f.EnginePID != pid {
		cr.OK = false
		cr.Severity = SeverityWarning
		cr.Drift = []string{fmt.Sprintf("engine degraded: liveness pid %d != engine.pid %d", f.EnginePID, pid)}
		return cr
	}

	if age := time.Since(f.HeartbeatAt); age > deps.staleAfter() {
		cr.OK = false
		cr.Severity = SeverityWarning
		cr.Drift = []string{fmt.Sprintf("engine degraded: stale heartbeat (%s old, max %s)", age.Truncate(time.Second), deps.staleAfter())}
		return cr
	}

	// Version skew: serve's own recorded binary_version (install.json,
	// refreshed from /healthz on restart — see checkDaemon) vs the engine
	// child's self-reported version in the liveness statusfile. Only
	// meaningful in split mode (we already know engine.pid is present here);
	// in monolith mode there's a single binary so this branch is unreachable.
	if state != nil && state.DaemonVersion != "" && f.Version != "" && state.DaemonVersion != f.Version {
		cr.OK = false
		cr.Severity = SeverityWarning
		cr.Drift = []string{fmt.Sprintf("version skew: serve=%s engine=%s (restart the engine child to pick up the new build)", state.DaemonVersion, f.Version)}
		return cr
	}

	return cr
}

// preSplitUnitTokens are the literal legacy-argument markers left behind in
// an OS service unit rendered before PR5 retargeted the templates from
// `daemon` to `serve` (launchd plist / systemd unit / Windows Task Scheduler
// XML respectively). Behavior-identical either way while split mode is off —
// this is purely informational.
var preSplitUnitTokens = []string{
	"<string>daemon</string>",       // launchd ProgramArguments
	"<Arguments>daemon</Arguments>", // Windows Task Scheduler
}

// looksLikePreSplitUnit reports whether unit content still literally execs
// the legacy `daemon` argument instead of `serve`.
func looksLikePreSplitUnit(content string) bool {
	for _, tok := range preSplitUnitTokens {
		if strings.Contains(content, tok) {
			return true
		}
	}
	// systemd's ExecStart is a single line ending in the argument, e.g.
	// "ExecStart=/usr/local/bin/grafel daemon" — check line-by-line rather
	// than a single substring so we don't false-match "daemon" appearing
	// elsewhere (e.g. Description=grafel knowledge-graph daemon).
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExecStart=") && strings.HasSuffix(line, " daemon") {
			return true
		}
	}
	return false
}

// checkPreSplitUnit reports (as SeverityInfo) when the installed OS service
// unit still literally execs the legacy `daemon` argument. Returns nil when
// no unit is installed, the unit can't be determined/read, or the unit
// already execs `serve` — this is advisory-only noise we don't want to add
// when there's nothing to say (mirrors checkStaleStagingDirs' nil-when-clean
// pattern).
func checkPreSplitUnit() *CheckResult {
	st, err := service.Status(service.Options{})
	if err != nil || st.UnitFile == "" {
		return nil
	}
	data, err := os.ReadFile(st.UnitFile)
	if err != nil {
		return nil
	}
	if !looksLikePreSplitUnit(string(data)) {
		return nil
	}
	cr := CheckResult{
		Surface:  "service-unit",
		OK:       false,
		Severity: SeverityInfo,
		Drift:    []string{fmt.Sprintf("%s still execs the legacy 'daemon' shim — it will retarget to 'serve' on the next `grafel update`/`grafel install`", st.UnitFile)},
	}
	return &cr
}

// ── Quick mode ────────────────────────────────────────────────────────────────

// QuickOptions configures the cheap quick-doctor check.
type QuickOptions struct {
	// StatePath is the path of install.json.  Defaults to DefaultStatePath().
	StatePath string

	// DaemonPort is the HTTP port for the daemon's /healthz endpoint.
	// Defaults to 47274.
	DaemonPort int

	// DaemonTimeout is the maximum wait for the /healthz call.
	// Defaults to 500ms — must be cheap.
	DaemonTimeout time.Duration

	// Out is where warnings are written.  Defaults to os.Stderr.
	Out io.Writer
}

func (o *QuickOptions) applyDefaults() error {
	if o.StatePath == "" {
		p, err := DefaultStatePath()
		if err != nil {
			return err
		}
		o.StatePath = p
	}
	if o.DaemonPort == 0 {
		o.DaemonPort = 47274
	}
	if o.DaemonTimeout == 0 {
		o.DaemonTimeout = 500 * time.Millisecond
	}
	if o.Out == nil {
		o.Out = os.Stderr
	}
	return nil
}

// RunQuickDoctor performs only the two cheap checks:
//  1. CLI SHA matches install.json
//  2. Daemon /healthz reachable (500ms timeout)
//
// On success it returns nil and prints nothing.
// On drift it prints a single one-line warning to opts.Out and returns nil —
// quick-doctor NEVER blocks the calling command with an error.
//
// Total budget: <50ms on a warm filesystem (SHA of a few-MB binary + 1 HTTP
// round trip with a 500ms cap).
func RunQuickDoctor(opts QuickOptions) error {
	if err := opts.applyDefaults(); err != nil {
		// Programming error — surface it.
		return err
	}

	state, err := ReadState(opts.StatePath)
	if err != nil || state == nil {
		// No install.json — silently skip; install hasn't run yet.
		return nil
	}

	var warnings []string

	// Check 1: CLI SHA (skip if in a git worktree).
	if state.CLI.Path != "" && state.CLI.SHA256 != "" && !isInGitWorktree() {
		actual, shaErr := sha256File(state.CLI.Path)
		if shaErr == nil && actual != state.CLI.SHA256 {
			warnings = append(warnings, "binary updated since last install (daemon still usable)")
		}
	}

	// Check 2: Daemon /healthz (cheap probe, non-blocking on failure).
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", opts.DaemonPort)
	client := &http.Client{Timeout: opts.DaemonTimeout}
	resp, daemonErr := client.Get(url)
	if daemonErr != nil {
		warnings = append(warnings, fmt.Sprintf("daemon unreachable at :%d", opts.DaemonPort))
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			warnings = append(warnings, fmt.Sprintf("daemon /healthz returned %d", resp.StatusCode))
		}
	}

	if len(warnings) > 0 {
		fmt.Fprintf(opts.Out, "grafel doctor: %s — run 'grafel doctor' for details\n",
			strings.Join(warnings, "; "))
	}

	return nil
}

// ── Rendering ─────────────────────────────────────────────────────────────────

// ANSI colour codes; suppressed when NO_COLOR is set.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiCyan   = "\033[36m"
)

func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

func colorize(code, s string) string {
	if !colorEnabled() {
		return s
	}
	return code + s + ansiReset
}

// RenderReport writes a human-readable coloured report to w.
func RenderReport(w io.Writer, report *DoctorReport) {
	for _, c := range report.Checks {
		var prefix string
		if c.OK {
			prefix = colorize(ansiGreen, "[ ok ]")
		} else {
			switch c.Severity {
			case SeverityCritical:
				prefix = colorize(ansiRed, "[FAIL]")
			case SeverityWarning:
				prefix = colorize(ansiYellow, "[warn]")
			default:
				prefix = colorize(ansiCyan, "[info]")
			}
		}
		fmt.Fprintf(w, "%s %s\n", prefix, c.Surface)
		for _, d := range c.Drift {
			fmt.Fprintf(w, "       %s\n", d)
		}
	}

	if !report.OK {
		fmt.Fprintf(w, "\n%s\n", colorize(ansiRed, "Run `grafel install` to fix."))
	} else {
		// Check for warnings.
		hasWarn := false
		for _, c := range report.Checks {
			if !c.OK && c.Severity == SeverityWarning {
				hasWarn = true
				break
			}
		}
		if hasWarn {
			fmt.Fprintf(w, "\n%s\n", colorize(ansiYellow, "Warnings detected. Run `grafel doctor` for details."))
		} else {
			fmt.Fprintf(w, "\n%s\n", colorize(ansiGreen, "All checks passed."))
		}
	}
}

// ── manifest helpers (re-exported for tests) ─────────────────────────────────

// BuildManifestPublic is a test-accessible wrapper around the internal buildManifest.
// It returns a map of relative-path → hex SHA-256 for every file under root.
func BuildManifestPublic(root string) (map[string]string, error) {
	return buildManifest(root)
}

// sha256FileSingle hashes a single file and returns its hex SHA-256.
// Exported for test helpers that need to tamper with files.
func SHA256FilePublic(path string) (string, error) {
	return sha256File(path)
}

// sha256BytesPublic hashes a byte slice.
func SHA256BytesPublic(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ── walk helper ───────────────────────────────────────────────────────────────

// walkFiles yields every regular file path under root, relative to root,
// with forward slashes. Used by checkStaleStagingDirs (and tests).
func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// gitignoreScanner checks a file's content for an entry.
// Exported for tests.
func HasGitignoreEntry(content []byte, entry string) bool {
	entry = strings.TrimSpace(entry)
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == entry {
			return true
		}
	}
	return false
}

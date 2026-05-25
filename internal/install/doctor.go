// Package install — doctor.go
//
// RunDoctor and its helpers implement `archigraph doctor` (#2211).
//
// Doctor reads ~/.archigraph/install.json as ground truth and compares it
// against live state across five surfaces:
//
//   - CLI binary SHA-256 (Critical)
//   - Daemon /healthz reachability + version (Critical)
//   - Skills per-file SHA manifests (Critical)
//   - MCP registration in detected .claude.json files (Critical)
//   - Conventions per-file SHA manifests (Warning)
//   - .gitignore contains /.archigraph/ in tracked repos (Warning)
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
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/install/mcpreg"
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
		report.Remediation = "Run: archigraph install"
		return report, nil
	}

	if state == nil {
		report.OK = false
		report.Checks = []CheckResult{{
			Surface:  "install.json",
			OK:       false,
			Severity: SeverityCritical,
			Drift:    []string{fmt.Sprintf("install.json not found at %s — archigraph has not been installed", opts.StatePath)},
		}}
		report.Remediation = "Run: archigraph install"
		return report, nil
	}

	// Derive skills dir from ClaudeConfigDirs or auto-detection.
	skillsDir := opts.SkillsDir
	if skillsDir == "" {
		claudeDirs := mcpreg.DetectClaudeConfigDirs(opts.ClaudeConfigDirs)
		if len(claudeDirs) > 0 {
			skillsDir = filepath.Join(filepath.Dir(claudeDirs[0]), "skills")
		}
	}

	// ── Check 1: CLI binary SHA ─────────────────────────────────────────────
	report.Checks = append(report.Checks, checkCLI(state))

	// ── Check 2: Daemon /healthz ────────────────────────────────────────────
	report.Checks = append(report.Checks, checkDaemon(state, opts.DaemonPort, opts.DaemonTimeout))

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

	// ── Check 5: Conventions per-file SHA ──────────────────────────────────
	if len(state.Skills) > 0 {
		report.Checks = append(report.Checks, checkConventions(state, skillsDir)...)
	}

	// ── Check 6: .gitignore in tracked repos ────────────────────────────────
	for _, repo := range state.Gitignore.Repos {
		report.Checks = append(report.Checks, checkGitignore(repo))
	}

	// ── Check 7: Stale staging dirs ─────────────────────────────────────────
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
		report.Remediation = "Run: archigraph install"
	}

	return report, nil
}

// checkCLI compares the running binary SHA against install.json.
func checkCLI(state *State) CheckResult {
	cr := CheckResult{Surface: "cli", OK: true}

	if state.CLI.Path == "" || state.CLI.SHA256 == "" {
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{"install.json has no CLI record (partial install?)"}
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
		cr.OK = false
		cr.Severity = SeverityCritical
		cr.Drift = []string{fmt.Sprintf("sha256 mismatch: binary=%s install=%s", actual[:16], state.CLI.SHA256[:16])}
	}
	return cr
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
		cr.Drift = []string{fmt.Sprintf("%s is not a symlink (replaced with a copy?); run `archigraph install --dev --force` to restore", skillDst)}
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

// checkMCP verifies that the archigraph MCP entry is present in every
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
			cr.Drift = append(cr.Drift, fmt.Sprintf("%s: archigraph entry absent (not in install record)", cfgPath))
		}
	}

	if len(cr.Drift) > 0 && cr.Severity == SeverityCritical {
		cr.OK = false
	}
	return cr
}

// mcpEntryDrift returns (missing=true, "") when the archigraph entry is absent,
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

// checkConventions checks per-file SHA for convention files inside the
// generate-docs skill (conventions/ subdirectory).
func checkConventions(state *State, skillsDir string) []CheckResult {
	genDocsRecord, ok := state.Skills["generate-docs"]
	if !ok {
		return nil // generate-docs not installed — skip
	}

	var results []CheckResult

	if skillsDir == "" {
		results = append(results, CheckResult{
			Surface:  "conventions/generate-docs",
			OK:       false,
			Severity: SeverityWarning,
			Drift:    []string{"skills directory not determined"},
		})
		return results
	}

	conventionsDir := filepath.Join(skillsDir, "generate-docs", "conventions")
	if _, err := os.Stat(conventionsDir); err != nil {
		// conventions dir doesn't exist — warn
		results = append(results, CheckResult{
			Surface:  "conventions/generate-docs",
			OK:       false,
			Severity: SeverityWarning,
			Drift:    []string{"conventions directory missing"},
		})
		return results
	}

	// Build live manifest of only conventions/ files from the skill record.
	var conventionDrift []string
	for relPath, installedSHA := range genDocsRecord.Files {
		if !strings.HasPrefix(relPath, "conventions/") {
			continue
		}
		absPath := filepath.Join(skillsDir, "generate-docs", filepath.FromSlash(relPath))
		data, err := os.ReadFile(absPath)
		if err != nil {
			conventionDrift = append(conventionDrift, fmt.Sprintf("%s missing", relPath))
			continue
		}
		live := sha256Bytes(data)
		if live != installedSHA {
			conventionDrift = append(conventionDrift, fmt.Sprintf("%s sha mismatch", relPath))
		}
	}

	cr := CheckResult{
		Surface:  "conventions/generate-docs",
		OK:       len(conventionDrift) == 0,
		Severity: SeverityWarning,
		Drift:    conventionDrift,
	}
	results = append(results, cr)
	return results
}

// checkGitignore verifies that the .gitignore in repoRoot contains /.archigraph/.
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

	if !hasGitignoreEntry(data, archigraphGitignoreEntry) {
		cr.OK = false
		cr.Drift = []string{fmt.Sprintf("/.archigraph/ missing from %s", gitignorePath)}
	}
	return cr
}

// checkStaleStagingDirs looks for .archigraph/staging/<run_id>/ directories
// older than 7 days under the archigraph state root.
// Returns nil when no stale dirs exist (avoids adding a check with no content).
func checkStaleStagingDirs(statePath string) *CheckResult {
	archigraphDir := filepath.Dir(statePath)
	stagingDir := filepath.Join(archigraphDir, "staging")

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

	// Check 1: CLI SHA.
	if state.CLI.Path != "" && state.CLI.SHA256 != "" {
		actual, shaErr := sha256File(state.CLI.Path)
		if shaErr == nil && actual != state.CLI.SHA256 {
			warnings = append(warnings, "binary SHA mismatch (reinstall recommended)")
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
		fmt.Fprintf(opts.Out, "archigraph doctor: %s — run 'archigraph doctor' for details\n",
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
		fmt.Fprintf(w, "\n%s\n", colorize(ansiRed, "Run `archigraph install` to fix."))
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
			fmt.Fprintf(w, "\n%s\n", colorize(ansiYellow, "Warnings detected. Run `archigraph doctor` for details."))
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

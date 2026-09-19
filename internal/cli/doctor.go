package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/envguard"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/version"
)

const (
	statusOK   = "[ ok ]"
	statusWarn = "[warn]"
	statusFail = "[FAIL]"
)

// reportIsolationFinding writes the partial-isolation state, if any, as a
// doctor finding and reports whether it wrote anything.
//
// The wording deliberately is NOT the guard's refusal text: nothing is being
// refused here, and "refusing to run" inside a doctor report would be a lie.
// It carries the same two facts — what is inconsistent, and what that does —
// from envguard.Result's verdict-neutral fields.
func reportIsolationFinding(w io.Writer) bool {
	res := envguard.Check(os.LookupEnv, envguard.RealUserHome())
	if res.Verdict == envguard.VerdictOK || res.Summary == "" {
		return false
	}
	fmt.Fprintf(w, "%s isolation: %s\n", statusWarn, res.Summary)
	fmt.Fprintf(w, "       %s\n", res.Detail)
	fmt.Fprintf(w, "       Everything below this line is reported against THAT environment.\n\n")
	return true
}

func newDoctorCmd() *cobra.Command {
	var killStale bool
	var auditDocs bool
	var refFlag string
	var jsonOut bool
	var quick bool
	var deep bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks across all groups",
		Long: `Run health checks across all registered groups.

With no flags: runs the full install-drift doctor (CLI SHA, daemon, skills,
MCP, conventions, .gitignore, stale staging dirs) followed by the runtime
health report. Exits non-zero when any Critical check fails.

--json       Machine-readable JSON output (stable schema_version=1); suitable
             for CI pipelines. Exits non-zero on Critical drift.

--quick      Cheap two-check mode: CLI SHA + daemon /healthz (500ms cap).
             Prints a one-line warning on drift but never blocks the caller.
             Used internally by every CLI command's entry point.

--deep       Opt into full-load recomputation of the enriched health report.
             The default path already loads each repo graph at most once (a
             single O(E) pass — no multi-minute hang on 250k+-entity graphs,
             #5689) and sources counts from the live graph, so --deep no longer
             changes the numbers when the load succeeds. Retained for its
             documented full-recompute semantics.

--ref        Filter runtime graph-state checks to a specific ref.
--ref @all   Check health across every known ref.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			// ── Isolation state (#6331) ───────────────────────────────────
			// doctor is exempt from the isolation guard's refusal (see
			// internal/cli/isolation_guard.go) because it is the command you
			// run to diagnose this very state. Exempt means "report it", not
			// "ignore it" — so it goes out as a finding, on stderr so --json
			// stdout stays a clean document.
			if jsonOut {
				reportIsolationFinding(cmd.ErrOrStderr())
			} else {
				reportIsolationFinding(w)
			}

			// ── Quick mode ────────────────────────────────────────────────
			// Only runs the two cheap checks and exits.
			if quick {
				return runQuickDoctorCmd(w)
			}

			// ── Install drift doctor (full or JSON) ───────────────────────
			// Always run the install-drift checks first so critical binary /
			// skill drift is reported before the runtime health section.
			installReport, err := install.RunDoctor(install.DoctorOptions{})
			if err != nil {
				fmt.Fprintf(w, "[warn] install doctor error: %v\n", err)
			} else if jsonOut {
				// JSON-only mode: emit the report and exit.
				return emitDoctorJSON(w, installReport)
			} else {
				// Human-readable prefix section.
				fmt.Fprintf(w, "--- Install Drift Checks ---\n")
				install.RenderReport(w, installReport)
				fmt.Fprintln(w)
			}

			// ── Runtime health (existing doctor logic) ────────────────────
			resolvedRef, isAll, err := resolveRef(refFlag, true /* @all ok — doctor is read-only */)
			if err != nil {
				return err
			}
			if resolvedRef != "" {
				fmt.Fprintf(w, "Note: running doctor for ref %q.\n\n", resolvedRef)
			} else if isAll {
				fmt.Fprintf(w, "Note: --ref @all — running doctor across all known refs.\n\n")
			}
			if err := runDoctor(w, deep); err != nil {
				return err
			}
			if auditDocs {
				if err := runDoctorAuditDocs(w); err != nil {
					return err
				}
			}
			if err := runDoctorStaleDaemons(w, killStale); err != nil {
				return err
			}

			// Exit non-zero when any Critical install check failed.
			if installReport != nil && !installReport.OK {
				return fmt.Errorf("critical drift detected — run 'grafel install' to fix")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&killStale, "kill-stale", false,
		"kill stale grafel daemons (default: dry-run list only)")
	cmd.Flags().BoolVar(&auditDocs, "audit-docs", false,
		"detect in-repo docgen output (storage discipline #2190); reports without moving anything")
	cmd.Flags().StringVar(&refFlag, "ref", "", refFlagUsage)
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"emit machine-readable JSON report (schema_version=1); exits non-zero on Critical drift")
	cmd.Flags().BoolVar(&quick, "quick", false,
		"run only the two cheap checks: CLI SHA + daemon /healthz (500ms); never blocks caller")
	cmd.Flags().BoolVar(&deep, "deep", false,
		"opt into full-load recomputation of the enriched health report. The default path "+
			"already loads each repo graph at most once and sources counts from the live graph, "+
			"so this no longer changes the numbers when the load succeeds; retained for its "+
			"documented full-recompute semantics (#5689)")
	return cmd
}

// runQuickDoctorCmd runs quick-doctor and writes the one-line warning to w.
// It always returns nil — quick mode never blocks commands.
func runQuickDoctorCmd(w io.Writer) error {
	_ = install.RunQuickDoctor(install.QuickOptions{Out: w})
	return nil
}

// doctorJSONGroup is one group's runtime findings in the --json payload.
type doctorJSONGroup struct {
	Name string `json:"name"`
	// UnsupportedLanguages is the UNCAPPED row list (#6338). The human table
	// caps at UnsupportedMaxRows and tells the reader to come here for the
	// rest, so this must never be capped or that instruction becomes another
	// confidently-wrong statement.
	UnsupportedLanguages []UnsupportedRow `json:"unsupported_languages,omitempty"`
}

// doctorJSONReport is what `grafel doctor --json` emits.
//
// install.DoctorReport is EMBEDDED rather than nested, so every key that
// payload carried before keeps its name, its value and its position, and the
// only change any existing consumer sees is one extra top-level "groups" key.
// (A map[string]json.RawMessage merge would have worked too, but Go sorts map
// keys on marshal and would have reordered the whole document.)
type doctorJSONReport struct {
	*install.DoctorReport
	Groups []doctorJSONGroup `json:"groups,omitempty"`
}

// doctorUnsupportedGroups collects the per-group unsupported-language rows for
// the JSON payload. Sidecar reads only — see ComputeDoctorUnsupported for why
// this does not go through ComputeDoctorHealth. A registry that cannot be read
// yields no groups: --json is primarily an install-drift check and must not
// start failing because the runtime registry is missing.
func doctorUnsupportedGroups() []doctorJSONGroup {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	var out []doctorJSONGroup
	for _, h := range ComputeDoctorUnsupported(groups) {
		rows := UnsupportedRows(h.UnsupportedExt, DoctorUnsupportedMinFiles)
		if len(rows) == 0 {
			continue // print/emit nothing when clean, same policy as the table
		}
		out = append(out, doctorJSONGroup{Name: h.GroupName, UnsupportedLanguages: rows})
	}
	return out
}

// emitDoctorJSON marshals report as indented JSON to w and returns a non-nil
// error (to trigger non-zero exit) when report.OK is false.
func emitDoctorJSON(w io.Writer, report *install.DoctorReport) error {
	b, err := json.MarshalIndent(doctorJSONReport{
		DoctorReport: report,
		Groups:       doctorUnsupportedGroups(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal doctor report: %w", err)
	}
	fmt.Fprintf(w, "%s\n", b)
	if !report.OK {
		return fmt.Errorf("critical drift detected — run 'grafel install' to fix")
	}
	return nil
}

// runDoctorAuditDocs checks every registered group for in-repo docgen output
// and reports offending directories to w. It never moves or deletes anything.
func runDoctorAuditDocs(w io.Writer) error {
	groups, err := registry.Groups()
	if err != nil {
		fmt.Fprintf(w, "%s audit-docs: registry unavailable: %v\n", statusWarn, err)
		return nil
	}

	fmt.Fprintf(w, "\n--- Storage Discipline Audit (#2190) ---\n")
	totalOffenders := 0

	for _, g := range groups {
		dirs, err := auditDocgenForGroup(w, g.Name)
		if err != nil {
			fmt.Fprintf(w, "  %s group %s: %v\n", statusWarn, g.Name, err)
			continue
		}
		if len(dirs) == 0 {
			fmt.Fprintf(w, "  %s group %s: no in-repo docgen output\n", statusOK, g.Name)
			continue
		}
		fmt.Fprintf(w, "  %s group %s: %d in-repo docgen director(ies) found\n", statusWarn, g.Name, len(dirs))
		for _, d := range dirs {
			var markers []string
			for _, name := range docgenHeuristics {
				if _, err := os.Stat(filepath.Join(d, name)); err == nil {
					markers = append(markers, name)
				}
			}
			fmt.Fprintf(w, "       %s  (markers: %s)\n", d, strings.Join(markers, ", "))
		}
		totalOffenders += len(dirs)
	}

	if totalOffenders > 0 {
		fmt.Fprintf(w, "\nRun 'grafel docgen migrate-in-repo' per group to move output to the grafel store.\n")
	}
	return nil
}

// runDoctor runs every health check and reports to w. It returns nil
// even when checks fail — the report itself is the user signal.
func runDoctor(w io.Writer, deep bool) error {
	fmt.Fprintf(w, "%s grafel %s\n", statusOK, version.String())

	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(w, "%s grafel binary: %v\n", statusWarn, err)
	} else {
		fmt.Fprintf(w, "%s grafel binary: %s\n", statusOK, bin)
	}

	// Reindex-storm mitigations (#5231): surface whether the daemon's
	// memory- and CPU-saving indexing paths are active so operators can
	// confirm them at a glance. Both are read from the local process env,
	// which a co-located daemon shares.
	printIndexingModes(w)

	regPath, _ := registry.RegistryPath()
	groups, err := registry.Groups()
	if err != nil {
		fmt.Fprintf(w, "%s registry %s: %v\n", statusFail, regPath, err)
		return nil
	}
	fmt.Fprintf(w, "%s registry %s (%d group(s))\n", statusOK, regPath, len(groups))

	// Run basic config checks
	for _, g := range groups {
		fmt.Fprintf(w, "\nGroup: %s\n", g.Name)
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			fmt.Fprintf(w, "  %s config %s: %v\n", statusFail, g.ConfigPath, err)
			continue
		}
		fmt.Fprintf(w, "  %s config %s\n", statusOK, g.ConfigPath)
		for _, r := range cfg.Repos {
			checkRepo(w, r)
		}
		stateDir, _ := registry.StateDirFor(g.Name)
		if _, err := os.Stat(stateDir); err == nil {
			fmt.Fprintf(w, "  %s state dir %s\n", statusOK, stateDir)
		} else {
			fmt.Fprintf(w, "  %s state dir %s: %v\n", statusWarn, stateDir, err)
		}
	}

	// MCP entries.
	for _, tool := range []mcpreg.Tool{mcpreg.ClaudeCode, mcpreg.Windsurf} {
		p, _ := mcpreg.SettingsPath(tool)
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(w, "%s mcp %s: not present\n", statusWarn, tool)
		} else {
			fmt.Fprintf(w, "%s mcp %s: %s\n", statusOK, tool, p)
		}
	}

	// Print enriched health report for each group
	fmt.Fprintf(w, "\n--- Enriched Health Report ---\n")
	healthReports := ComputeDoctorHealth(groups, deep)
	PrintDoctorHealth(w, healthReports)

	return nil
}

// staleProcess describes an grafel process that is a candidate for cleanup.
type staleProcess struct {
	PID      int
	PPID     int
	Exe      string
	IsOrphan bool // PPID == 1 (adopted by launchd/systemd after parent exited)
	IsTmp    bool // binary path under /tmp
}

// killGuidance returns the platform-appropriate command to kill a stale daemon.
// On Windows it suggests taskkill; on all unix-like systems it suggests kill(1).
func killGuidance() string {
	// runtime.GOOS check is intentionally inline so the compiler sees a
	// constant string per platform — no import of "runtime" needed in this file.
	return `grafel doctor --kill-stale`
}

// runDoctorStaleDaemons scans running processes for stale grafel daemons:
//   - any grafel process with PPID=1 AND binary path under /tmp
//   - any grafel daemon process running from a different binary than self
//
// In dry-run mode (kill=false) it lists them. With kill=true it sends SIGTERM.
func runDoctorStaleDaemons(w io.Writer, kill bool) error {
	myPID := os.Getpid()

	selfExe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(w, "%s stale-daemon scan: os.Executable() failed: %v\n", statusWarn, err)
		return nil
	}

	procs, err := scanGrafelProcs(myPID)
	if err != nil {
		fmt.Fprintf(w, "%s stale-daemon scan: %v\n", statusWarn, err)
		return nil
	}

	var stale []staleProcess
	for _, p := range procs {
		if isStaleProc(p, selfExe) {
			stale = append(stale, p)
		}
	}

	if len(stale) == 0 {
		fmt.Fprintf(w, "%s stale daemons: none found\n", statusOK)
		return nil
	}

	action := "would kill"
	if kill {
		action = "killing"
	}
	fmt.Fprintf(w, "\nStale grafel processes (%s):\n", action)
	for _, p := range stale {
		orphanNote := ""
		if p.IsOrphan {
			orphanNote = " [orphan: PPID=1]"
		}
		tmpNote := ""
		if p.IsTmp {
			tmpNote = " [/tmp binary]"
		}
		fmt.Fprintf(w, "  pid=%-6d ppid=%-6d %s%s%s\n", p.PID, p.PPID, p.Exe, orphanNote, tmpNote)
		if kill {
			if kerr := killProc(p.PID); kerr != nil {
				fmt.Fprintf(w, "    kill: %v\n", kerr)
			} else {
				fmt.Fprintf(w, "    killed pid %d\n", p.PID)
			}
		}
	}

	if !kill {
		fmt.Fprintf(w, "\nRun 'grafel doctor --kill-stale' to terminate these processes.\n")
	}
	return nil
}

// isStaleProc reports whether a scanned process is a candidate for SIGTERM by
// `grafel doctor --kill-stale`.
//
// It used to live inline in runDoctorStaleDaemons, with a hand-copied twin in
// doctor_staleness_test.go; the twin was the only thing any test exercised, so
// the shipped predicate was ungraded. It is production code now and the tests
// call THIS function.
func isStaleProc(p staleProcess, selfExe string) bool {
	// IDENTITY GATE (#7268), a precondition for EVERY criterion below.
	//
	// scanGrafelProcs finds its candidates with process.FindByName("grafel"),
	// which is a case-insensitive substring match over the whole exec path —
	// it matches any binary that merely LIVES under a directory named after
	// this project. Both criteria below then decided kill-eligibility from
	// path properties (a "daemon" substring, a /tmp prefix) that a stranger's
	// binary can satisfy just as easily:
	//
	//   /Users/jane smith/Library/grafel-daemon-helper/bin/helper
	//
	// was eligible for SIGTERM. That is the same class of false positive
	// #1719 fixed on the daemon path, where a project directory named
	// "grafel" made every node_modules esbuild look canonical; the answer
	// there was an exact basename match, and this call site simply never
	// adopted it.
	//
	// So ask the identity question with the same gate findCanonicalDaemon
	// uses, and ask it FIRST. The criteria that follow stay exactly as they
	// were: the gate can only narrow what they select, never widen it.
	if !daemon.IsCanonicalBinaryPath(p.Exe) {
		return false
	}
	// Stale criterion 1: PPID=1 (launchd/systemd orphan) + binary under /tmp
	if p.PPID == 1 && p.IsTmp {
		return true
	}
	// Stale criterion 2: daemon process running from a different binary than self.
	//
	// NOT WIDENED HERE, deliberately. "daemon" is an ARGUMENT, not part of the
	// exec path: a manual fork spawns it as `<bin> daemon` (watcher_ctl.go),
	// while the INSTALLED service passes `serve` instead (the launchd plist's
	// ProgramArguments is {BinPath, "serve"}). Either spelling is an argument,
	// not a path component, while Info.Exe is the executable path on
	// every platform (/proc/<pid>/exe on Linux, `ps -eo comm` on darwin — the
	// `ps aux` fallback takes argv[0] only, see #7259). So this substring fires
	// for a genuine daemon only when its INSTALL DIRECTORY happens to contain
	// "daemon", and never because the process is a daemon. Replacing it with
	// the identity gate alone would make every other grafel process — a
	// concurrent `grafel status`, the user's mcp-bridge — kill-eligible, which
	// is a widening of a kill path and needs its own decision. Filed as a
	// finding on #7268 rather than changed under cover of this fix.
	//
	// p.Exe != selfExe is string equality, not identity: a daemon started via
	// a symlink reports the link path while os.Executable resolves it, so the
	// two can differ for one binary. Self is excluded by PID in
	// scanGrafelProcs, so this cannot select the running process; the reachable
	// consequence is that a sibling launched by a different path spelling is
	// treated as a different binary. The relative-path spelling is closed by
	// the absoluteness half of the identity gate above. Also a finding, not
	// fixed here.
	if strings.Contains(strings.ToLower(p.Exe), "daemon") && p.Exe != selfExe {
		return true
	}
	return false
}

// findProcs is process.FindByName, indirected through a package-level variable
// so tests can drive the REAL runDoctorStaleDaemons — selection AND the kill
// loop's output — with a synthetic process table. Same seam, same reason, as
// daemon.findProcs (internal/daemon/selfdefense.go).
//
// Without it isStaleProc was gradable but its ONLY CONSUMER was not: changing
// the call site in runDoctorStaleDaemons to `if isStaleProc(p, selfExe) ||
// p.PID != 0` — SIGTERM to every process FindByName returns, which is exactly
// the population #7268 exists to protect — left ./internal/cli green. The
// platform implementations read /proc or shell out to ps, so the only process
// table a test could otherwise observe is whatever happens to be running on
// the test machine. Never reassigned in production code.
var findProcs = process.FindByName

// killProc is process.KillGuarded, indirected for the same reason findProcs is:
// so the kill BRANCH of runDoctorStaleDaemons can be graded.
//
// The DEFAULT is KillGuarded, not Kill, and that is load-bearing. A seam only
// protects the tests that remember to install it, and round 4 of #7268 asserted
// in a comment that every test here did — a claim that was false at package
// scope on the day it was written (doctor_staleness_test.go drove this same
// function with no seam installed, reaching the real kill seam and the host
// process table). KillGuarded makes the protection a property of THIS LINE
// instead: under `go test` it panics naming the pid rather than signalling.
//
// Until this existed, no test could reach the SIGTERM path at all — the only
// safe way to drive the function was kill=false, which skips it — so `killing`
// vs `would kill`, the error reporting and the "killed pid N" line were all
// ungraded, and the kill=false guard in the tests was itself the only thing
// standing between a synthetic process table of invented PIDs (31001+) and
// SIGTERM to whatever really holds those PIDs on the host. That made the test
// suite a hazard, not just under-covered. Tests point this at a recorder.
// Never reassigned in production code.
var killProc = process.KillGuarded

// scanGrafelProcs uses the cross-platform process package to find all
// running grafel processes except myPID.
//
// On windows this always returns an error: process.FindByName is unsupported
// there, so the whole stale-daemon scan is unreachable in production on that
// platform (the findProcs seam deliberately bypasses that for tests).
func scanGrafelProcs(myPID int) ([]staleProcess, error) {
	infos, err := findProcs("grafel")
	if err != nil {
		return nil, fmt.Errorf("process scan: %w", err)
	}
	var result []staleProcess
	for _, p := range infos {
		if p.PID == myPID {
			continue
		}
		exe := p.Exe
		if exe == "" {
			exe = p.Name
		}
		result = append(result, staleProcess{
			PID:      p.PID,
			PPID:     p.PPID,
			Exe:      exe,
			IsOrphan: p.PPID == 1,
			// The `|| exe == "/tmp"` arm this used to carry is deleted (#7268).
			// It was an ungraded permissive branch on a SIGTERM path whose only
			// defence was a comment: exe == "/tmp" implies
			// filepath.Base(exe) == "tmp", which is not in
			// daemon.canonicalBasenames (pinned to exactly {"grafel"}), so
			// IsCanonicalBinaryPath rejected the path before either reader of
			// IsTmp — criterion 1 and the printed [/tmp binary] note, both
			// post-gate — could ever see it. No mutant could kill it and no
			// fixture could reach it.
			IsTmp: strings.HasPrefix(exe, "/tmp/"),
		})
	}
	return result, nil
}

// printIndexingModes reports whether the two reindex-storm mitigations
// (#5231) are active: incremental file-level reindex and subprocess
// (out-of-daemon) indexing. Both are derived from the local process
// environment, which a co-located daemon inherits at start.
func printIndexingModes(w io.Writer) {
	var cfg *extractor.ExtractorConfig // nil → IsIncrementalEnabled reads env
	incremental := cfg.IsIncrementalEnabled()
	subprocess := sched.SubprocessIndexEnabled()

	// Resource-safe defaults (v0.1.1): incremental reindex ON (#5231) and
	// subprocess indexer ON (CPU-capped child per reindex). Both are opt-out
	// via their env vars set to 0/false.
	fmt.Fprintf(w, "%s incremental reindex: %s (GRAFEL_INCREMENTAL_REINDEX; default on)\n",
		statusOK, onOff(incremental))
	fmt.Fprintf(w, "%s subprocess indexer: %s (GRAFEL_SUBPROCESS_INDEXER; default on)\n",
		statusOK, onOff(subprocess))

	// Go soft memory limit (#5237). Report the resolved limit + where it
	// came from so operators can see whether GOMEMLIMIT / the env override /
	// the fraction-of-RAM default is in effect.
	// #6045: report the installation-wide total plus the per-plane shares —
	// in split mode two processes divide this budget.
	memVal, memSrc := memLimitDescription()
	fmt.Fprintf(w, "%s go soft mem limit: %s (%s; GRAFEL_DAEMON_MEMLIMIT_MB)\n",
		statusOK, memVal, memSrc)
}

func checkRepo(w io.Writer, r registry.Repo) {
	if _, err := os.Stat(r.Path); err != nil {
		fmt.Fprintf(w, "  %s repo %s (%s): %v\n", statusFail, r.Slug, r.Path, err)
		return
	}
	// Resolve the enclosing git root (walk up) before deciding .git is missing.
	// In a single-.git monorepo the only .git lives at the repo root, so a
	// module subdir has no .git of its own — statting a literal .git in r.Path
	// would falsely flag every module (#5675).
	if !gitmeta.HasGitDirInTree(r.Path) {
		fmt.Fprintf(w, "  %s repo %s: missing .git\n", statusWarn, r.Slug)
	} else {
		fmt.Fprintf(w, "  %s repo %s (%s)\n", statusOK, r.Slug, r.Stack.String())
	}
	jsonPath := daemon.GraphPathForRepo(r.Path)
	// #5915 J2 P2: GraphFBExistsForRepo is segment-set aware.
	// os.Stat(daemon.GraphFBPathForRepo(r.Path)) — the pattern this replaces —
	// only ever resolves a flat .fb path, which is absent for a segment-set
	// repo (graph.<gen>/ dir + manifest.json, no flat .fb), so a fully-indexed
	// segmented repo would wrongly report "no graph found".
	hasFB := daemon.GraphFBExistsForRepo(r.Path)
	hasJSON := func() bool { _, e := os.Stat(jsonPath); return e == nil }()
	switch {
	case hasFB && hasJSON:
		fmt.Fprintf(w, "         graph.fb + graph.json present (dual-write active)\n")
	case hasFB:
		fmt.Fprintf(w, "         graph.fb present (--skip-json mode)\n")
	case hasJSON:
		// ADR-0016 flip-day (#808): old install with only graph.json.
		// Suggest a re-index so graph.fb is written.
		fmt.Fprintf(w, "         graph.json present (graph.fb missing — run 'grafel index' to generate the binary graph)\n")
	default:
		fmt.Fprintf(w, "         no graph found — run 'grafel index %s' to build\n", r.Path)
	}
}

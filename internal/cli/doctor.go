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
	"github.com/cajasmota/grafel/internal/extractor"
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

func newDoctorCmd() *cobra.Command {
	var killStale bool
	var auditDocs bool
	var refFlag string
	var jsonOut bool
	var quick bool
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

--ref        Filter runtime graph-state checks to a specific ref.
--ref @all   Check health across every known ref.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

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
			if err := runDoctor(w); err != nil {
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
	return cmd
}

// runQuickDoctorCmd runs quick-doctor and writes the one-line warning to w.
// It always returns nil — quick mode never blocks commands.
func runQuickDoctorCmd(w io.Writer) error {
	_ = install.RunQuickDoctor(install.QuickOptions{Out: w})
	return nil
}

// emitDoctorJSON marshals report as indented JSON to w and returns a non-nil
// error (to trigger non-zero exit) when report.OK is false.
func emitDoctorJSON(w io.Writer, report *install.DoctorReport) error {
	b, err := json.MarshalIndent(report, "", "  ")
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
func runDoctor(w io.Writer) error {
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
	healthReports := ComputeDoctorHealth(groups)
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
		isStale := false
		// Stale criterion 1: PPID=1 (launchd/systemd orphan) + binary under /tmp
		if p.PPID == 1 && p.IsTmp {
			isStale = true
		}
		// Stale criterion 2: daemon process running from a different binary than self
		if strings.Contains(strings.ToLower(p.Exe), "daemon") && p.Exe != selfExe {
			isStale = true
		}
		if isStale {
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
			if kerr := process.Kill(p.PID); kerr != nil {
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

// scanGrafelProcs uses the cross-platform process package to find all
// running grafel processes except myPID.
func scanGrafelProcs(myPID int) ([]staleProcess, error) {
	infos, err := process.FindByName("grafel")
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
			IsTmp:    strings.HasPrefix(exe, "/tmp/") || exe == "/tmp",
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

	// Incremental defaults ON (#5231); subprocess defaults OFF (memory
	// isolation is opt-in until rollout completes).
	fmt.Fprintf(w, "%s incremental reindex: %s (GRAFEL_INCREMENTAL_REINDEX; default on)\n",
		statusOK, onOff(incremental))
	fmt.Fprintf(w, "%s subprocess indexer: %s (GRAFEL_SUBPROCESS_INDEXER; default off)\n",
		statusOK, onOff(subprocess))
}

func checkRepo(w io.Writer, r registry.Repo) {
	if _, err := os.Stat(r.Path); err != nil {
		fmt.Fprintf(w, "  %s repo %s (%s): %v\n", statusFail, r.Slug, r.Path, err)
		return
	}
	gitDir := filepath.Join(r.Path, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		fmt.Fprintf(w, "  %s repo %s: missing .git\n", statusWarn, r.Slug)
	} else {
		fmt.Fprintf(w, "  %s repo %s (%s)\n", statusOK, r.Slug, r.Stack.String())
	}
	jsonPath := daemon.GraphPathForRepo(r.Path)
	fbPath := daemon.GraphFBPathForRepo(r.Path)
	hasFB := func() bool { _, e := os.Stat(fbPath); return e == nil }()
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

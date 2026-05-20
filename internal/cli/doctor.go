package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/registry"
	"github.com/cajasmota/archigraph/internal/version"
)

const (
	statusOK   = "[ ok ]"
	statusWarn = "[warn]"
	statusFail = "[FAIL]"
)

func newDoctorCmd() *cobra.Command {
	var killStale bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks across all groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if err := runDoctor(w); err != nil {
				return err
			}
			return runDoctorStaleDaemons(w, killStale)
		},
	}
	cmd.Flags().BoolVar(&killStale, "kill-stale", false,
		"kill stale archigraph daemons (default: dry-run list only)")
	return cmd
}

// runDoctor runs every health check and reports to w. It returns nil
// even when checks fail — the report itself is the user signal.
func runDoctor(w io.Writer) error {
	fmt.Fprintf(w, "%s archigraph %s\n", statusOK, version.String())

	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(w, "%s archigraph binary: %v\n", statusWarn, err)
	} else {
		fmt.Fprintf(w, "%s archigraph binary: %s\n", statusOK, bin)
	}

	regPath, _ := registry.RegistryPath()
	groups, err := registry.Groups()
	if err != nil {
		fmt.Fprintf(w, "%s registry %s: %v\n", statusFail, regPath, err)
		return nil
	}
	fmt.Fprintf(w, "%s registry %s (%d group(s))\n", statusOK, regPath, len(groups))

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
	return nil
}

// staleProcess describes an archigraph process that is a candidate for cleanup.
type staleProcess struct {
	PID      int
	PPID     int
	Exe      string
	IsOrphan bool // PPID == 1 (adopted by launchd/systemd after parent exited)
	IsTmp    bool // binary path under /tmp
}

// runDoctorStaleDaemons scans running processes for stale archigraph daemons:
//   - any archigraph process with PPID=1 AND binary path under /tmp
//   - any archigraph daemon process running from a different binary than self
//
// In dry-run mode (kill=false) it lists them. With kill=true it sends SIGTERM.
func runDoctorStaleDaemons(w io.Writer, kill bool) error {
	myPID := os.Getpid()

	selfExe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(w, "%s stale-daemon scan: os.Executable() failed: %v\n", statusWarn, err)
		return nil
	}

	procs, err := scanArchigraphProcs(myPID)
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
	fmt.Fprintf(w, "\nStale archigraph processes (%s):\n", action)
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
			proc, ferr := os.FindProcess(p.PID)
			if ferr != nil {
				fmt.Fprintf(w, "    kill: FindProcess(%d): %v\n", p.PID, ferr)
				continue
			}
			if kerr := proc.Signal(syscall.SIGTERM); kerr != nil {
				fmt.Fprintf(w, "    kill: signal(%d): %v\n", p.PID, kerr)
			} else {
				fmt.Fprintf(w, "    killed pid %d\n", p.PID)
			}
		}
	}

	if !kill {
		fmt.Fprintf(w, "\nRun 'archigraph doctor --kill-stale' to terminate these processes.\n")
	}
	return nil
}

// scanArchigraphProcs runs `ps aux` and returns every archigraph process
// except the given myPID. It captures pid, ppid, and command/binary path.
func scanArchigraphProcs(myPID int) ([]staleProcess, error) {
	// Use ps with o flag to get pid, ppid, and full command line.
	// -ww on macOS/Linux prevents truncation of long command paths.
	out, err := exec.Command("ps", "-eo", "pid,ppid,comm").Output()
	if err != nil {
		// Fall back to ps aux which may truncate on some platforms.
		out, err = exec.Command("ps", "aux").Output()
		if err != nil {
			return nil, fmt.Errorf("ps: %w", err)
		}
		return parsePsAux(out, myPID), nil
	}
	return parsePsEo(out, myPID), nil
}

// parsePsEo parses `ps -eo pid,ppid,comm` output.
func parsePsEo(out []byte, myPID int) []staleProcess {
	var result []staleProcess
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // header
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !strings.Contains(strings.ToLower(fields[2]), "archigraph") {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == myPID {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		exe := fields[2]
		result = append(result, staleProcess{
			PID:      pid,
			PPID:     ppid,
			Exe:      exe,
			IsOrphan: ppid == 1,
			IsTmp:    strings.HasPrefix(exe, "/tmp/") || exe == "/tmp",
		})
	}
	return result
}

// parsePsAux parses `ps aux` output (fallback path). Field layout:
// USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND...
func parsePsAux(out []byte, myPID int) []staleProcess {
	var result []staleProcess
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), "archigraph") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid == myPID {
			continue
		}
		exe := fields[10]
		// ps aux doesn't give us PPID directly; treat unknown PPID as 0.
		result = append(result, staleProcess{
			PID:      pid,
			PPID:     0,
			Exe:      exe,
			IsOrphan: false, // can't determine without PPID
			IsTmp:    strings.HasPrefix(exe, "/tmp/") || exe == "/tmp",
		})
	}
	return result
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
		fmt.Fprintf(w, "  %s repo %s (%s)\n", statusOK, r.Slug, r.Stack)
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
		fmt.Fprintf(w, "         graph.json present (graph.fb missing — run 'archigraph index' to generate the binary graph)\n")
	default:
		fmt.Fprintf(w, "         no graph found — run 'archigraph index %s' to build\n", r.Path)
	}
}

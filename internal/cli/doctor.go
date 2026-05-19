package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks across all groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.OutOrStdout())
		},
	}
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

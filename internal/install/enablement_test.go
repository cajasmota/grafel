package install_test

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/registry"
)

// applyDryRun runs install.Apply in DryRun mode under an isolated HOME and
// returns the Result. DryRun writes nothing but populates Result the same
// way as a real install, so it is a faithful probe of the per-tool
// enablement wiring.
func applyDryRun(t *testing.T, tools []string) *install.Result {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, ".grafel"))

	repo := t.TempDir()
	cfg := &registry.GroupConfig{
		Name:  "demo",
		Repos: []registry.Repo{{Slug: "r", Path: repo}},
		Tools: tools,
	}
	res, err := install.Apply(install.Options{
		Group:   "demo",
		Config:  cfg,
		BinPath: "/usr/local/bin/grafel",
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}

// TestApply_DefaultEnablement_AllSixRulesFiles is the back-compat
// regression guard at the Apply boundary: with no Tools the rules-file set
// reported is exactly the historical six.
func TestApply_DefaultEnablement_AllSixRulesFiles(t *testing.T) {
	res := applyDryRun(t, nil)

	var repoPath string
	for p := range res.RulesFiles {
		repoPath = p
	}
	if repoPath == "" {
		t.Fatal("no repo recorded in RulesFiles")
	}
	got := append([]string{}, res.RulesFiles[repoPath]...)
	want := append([]string{}, rulesfiles.Targets...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default rules files = %v, want %v", got, want)
	}
}

// TestApply_RestrictedEnablement_OnlySubset proves a restricted Tools list
// writes only that subset's rules files.
func TestApply_RestrictedEnablement_OnlySubset(t *testing.T) {
	res := applyDryRun(t, []string{"cursor", "copilot"})

	var repoPath string
	for p := range res.RulesFiles {
		repoPath = p
	}
	got := append([]string{}, res.RulesFiles[repoPath]...)
	want := []string{".cursorrules", ".github/copilot-instructions.md"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restricted rules files = %v, want %v", got, want)
	}
}

// TestApply_MCPSettings_CursorWindsurfCodex proves that enabling the three
// #5254 MCP tools records their per-tool config paths (in the right format)
// in Result.MCPSettings, and that a tool without MCP (copilot) records none.
func TestApply_MCPSettings_CursorWindsurfCodex(t *testing.T) {
	res := applyDryRun(t, []string{"cursor", "windsurf", "codex", "copilot"})

	joined := strings.Join(res.MCPSettings, "\n")
	for _, want := range []string{
		filepath.Join(".cursor", "mcp.json"),
		filepath.Join(".codeium", "windsurf", "mcp_config.json"),
		filepath.Join(".codex", "config.toml"),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("MCPSettings missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, filepath.Join(".codex", "config.json")) {
		t.Fatalf("Codex MCP must be config.toml, not config.json:\n%s", joined)
	}
	// copilot has no MCP host; nothing copilot-specific should appear.
	if strings.Contains(joined, "copilot") {
		t.Fatalf("copilot should not contribute an MCP path:\n%s", joined)
	}
}

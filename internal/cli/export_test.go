package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// setupExportGroup writes a minimal registry group config plus an indexed
// graph.json for a single repo, all under temp dirs scoped to the test via
// env overrides. It returns the group name.
func setupExportGroup(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	xdg := t.TempDir()
	root := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(daemon.EnvRoot, root)

	const group = "exptest"

	repoPath := filepath.Join(root, "repoA")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write an indexed graph at the repo's state dir.
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := graph.Document{
		Version: graph.SchemaVersion,
		Repo:    "repoA",
		Entities: []graph.Entity{
			{ID: "a1", Name: "OrderService", Kind: "Class", SourceFile: "order.go", StartLine: 3},
			{ID: "a2", Name: "placeOrder", Kind: "Function", SourceFile: "order.go", StartLine: 9},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a2", ToID: "a1", Kind: "calls"},
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write the group fleet config at the path ConfigPathFor resolves.
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := registry.GroupConfig{
		Name:  group,
		Repos: []registry.Repo{{Slug: "repoA", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	if err := registry.SaveGroupConfig(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	return group
}

func runExport(t *testing.T, group string, args ...string) (string, error) {
	t.Helper()
	cmd := newExportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--group", group}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestExport_AllFourFormatsProduceOutput(t *testing.T) {
	group := setupExportGroup(t)

	cases := []struct {
		format string
		want   string // a substring that must appear in the file
	}{
		{"graphml", "<graphml"},
		{"cypher", "CREATE (n:"},
		{"svg", "<svg"},
		{"html", "<!DOCTYPE html>"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			outFile := filepath.Join(t.TempDir(), "out."+tc.format)
			if _, err := runExport(t, group, tc.format, "--out", outFile); err != nil {
				t.Fatalf("export %s: %v", tc.format, err)
			}
			b, err := os.ReadFile(outFile)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if len(b) == 0 {
				t.Fatalf("export %s produced empty output", tc.format)
			}
			if !strings.Contains(string(b), tc.want) {
				t.Errorf("export %s: output missing %q:\n%s", tc.format, tc.want, b)
			}
			// Both entities must appear by name.
			if !strings.Contains(string(b), "OrderService") || !strings.Contains(string(b), "placeOrder") {
				t.Errorf("export %s: missing entity names", tc.format)
			}
		})
	}
}

func TestExport_UnknownFormatErrors(t *testing.T) {
	group := setupExportGroup(t)
	_, err := runExport(t, group, "json")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error = %v, want 'unknown format'", err)
	}
}

func TestExport_TopNCapsNodeCount(t *testing.T) {
	group := setupExportGroup(t)
	// Cap to a single node; the SVG must then draw exactly one rect.
	outFile := filepath.Join(t.TempDir(), "out.svg")
	if _, err := runExport(t, group, "svg", "--top-N", "1", "--out", outFile); err != nil {
		t.Fatalf("export svg --top-N 1: %v", err)
	}
	b, _ := os.ReadFile(outFile)
	if got := strings.Count(string(b), "<rect x="); got != 1 {
		t.Errorf("--top-N 1: want 1 node rect, got %d", got)
	}
	if !strings.Contains(string(b), "top-N cap") {
		t.Errorf("missing top-N hidden-node notice")
	}
}

func TestExport_Deterministic(t *testing.T) {
	group := setupExportGroup(t)
	var prev string
	for i := 0; i < 2; i++ {
		out, err := runExport(t, group, "graphml")
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && out != prev {
			t.Errorf("graphml export not deterministic across runs")
		}
		prev = out
	}
}

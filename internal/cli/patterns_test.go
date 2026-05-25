package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
	"github.com/cajasmota/archigraph/internal/registry"
)

// withTempHome points ARCHIGRAPH_HOME at a fresh tmpdir and registers a
// single group so resolvePatternsDir() succeeds without flags.
func withTempHome(t *testing.T) (homeDir, patternsDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	// Register a group via registry.AddGroup (writes registry.json +
	// stub config). The config path doesn't need to exist for our
	// CLI — resolvePatternsDir reads memory_dir if present and
	// otherwise falls back to home/groups/<name>-patterns.
	cfgPath := filepath.Join(home, "groups", "testgroup.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"name":"testgroup","repos":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("testgroup", cfgPath); err != nil {
		t.Fatal(err)
	}

	patterns := filepath.Join(home, "groups", "testgroup-patterns")
	if err := os.MkdirAll(patterns, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, patterns
}

func seedPatterns(t *testing.T, dir string) []agentpatterns.Pattern {
	t.Helper()
	patterns := []agentpatterns.Pattern{
		{
			ID:           "approved00000001",
			Kind:         "AgentPattern",
			Category:     agentpatterns.CategoryCode,
			Trigger:      agentpatterns.Trigger{NaturalLanguage: "register a chi handler"},
			Confidence:   0.72,
			Observations: 4,
			IsCandidate:  false,
			Steps:        []string{"add route", "add handler", "add test"},
		},
		{
			ID:          "candidate0000001",
			Kind:        "AgentPattern",
			Category:    agentpatterns.CategoryCode,
			Trigger:     agentpatterns.Trigger{NaturalLanguage: "candidate guess"},
			Confidence:  0.4,
			IsCandidate: true,
		},
	}
	if err := agentpatterns.Save(dir, patterns); err != nil {
		t.Fatal(err)
	}
	return patterns
}

func TestPatternsListCommand(t *testing.T) {
	_, dir := withTempHome(t)
	seedPatterns(t, dir)

	cmd := newPatternsListCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "register a chi handler") {
		t.Fatalf("missing approved pattern row:\n%s", out)
	}
	if !strings.Contains(out, "candidate guess") {
		t.Fatalf("missing candidate row:\n%s", out)
	}
}

func TestPatternsShowCommand(t *testing.T) {
	_, dir := withTempHome(t)
	seedPatterns(t, dir)
	cmd := newPatternsShowCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if err := cmd.RunE(cmd, []string{"approved00000001"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"id": "approved00000001"`) {
		t.Fatalf("show output missing id field:\n%s", buf.String())
	}
}

func TestPatternsConfigCommand(t *testing.T) {
	_, dir := withTempHome(t)
	seedPatterns(t, dir)

	// First: list config (no args) — should print defaults.
	cmd := newPatternsConfigCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"per_subagent_threshold": 2`) {
		t.Fatalf("default config not printed:\n%s", buf.String())
	}

	// Now set a value.
	buf.Reset()
	if err := cmd.RunE(cmd, []string{"candidate_decay_days=180"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := agentpatterns.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CandidateDecayDays != 180 {
		t.Fatalf("config not persisted: want 180, got %d", cfg.CandidateDecayDays)
	}
}

func TestPatternsExportCommand(t *testing.T) {
	_, dir := withTempHome(t)
	seedPatterns(t, dir)

	repoDir := t.TempDir()
	cmd := newPatternsExportCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if err := cmd.Flags().Set("repo", repoDir); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), agentpatterns.BlockStartMarker) {
		t.Fatalf("marker missing:\n%s", data)
	}
	if !strings.Contains(string(data), "register a chi handler") {
		t.Fatalf("approved pattern not exported:\n%s", data)
	}
	if strings.Contains(string(data), "candidate guess") {
		t.Fatalf("candidate leaked into export:\n%s", data)
	}
}

func TestPatternsEditCommand_rejectsInvalidJSON(t *testing.T) {
	_, dir := withTempHome(t)
	seedPatterns(t, dir)

	// Stub EDITOR with a script/binary that corrupts the file.
	const goCorruptBody = `
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}`
	editor := writeEditorScript(t,
		`printf '{not json' > "$1"`,
		goCorruptBody,
	)
	t.Setenv("EDITOR", editor)

	cmd := newPatternsEditCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.RunE(cmd, []string{"approved00000001"})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid-JSON error, got %v", err)
	}
}

func TestPatternsEditCommand_savesValidEdit(t *testing.T) {
	_, dir := withTempHome(t)
	seedPatterns(t, dir)

	// Editor that bumps confidence to 0.95.
	const goBumpBody = `
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var p map[string]any
	if err := json.Unmarshal(data, &p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p["confidence"] = 0.95
	out, _ := json.Marshal(p)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}`
	editor := writeEditorScript(t,
		`python3 -c "import json,sys; p=json.load(open(sys.argv[1])); p['confidence']=0.95; json.dump(p, open(sys.argv[1],'w'))" "$1"`,
		goBumpBody,
	)
	t.Setenv("EDITOR", editor)

	cmd := newPatternsEditCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, []string{"approved00000001"}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	patterns, _ := agentpatterns.Load(dir)
	got := agentpatterns.ByID(patterns, "approved00000001")
	if got == nil || got.Confidence != 0.95 {
		t.Fatalf("edit not persisted: %+v", got)
	}
}

// writeEditorScript creates an editor stub for use as $EDITOR in tests.
//
// On Unix a shell script (shellBody) is written and made executable.
// On Windows .sh files cannot be executed; instead a Go source file is
// compiled to a .exe binary using the goBody snippet, which must be a
// series of statements that operate on the variable `path` (the file to
// edit) and may use os, fmt, and encoding/json from the scaffold.
func writeEditorScript(t *testing.T, shellBody, goBody string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		script := filepath.Join(t.TempDir(), "editor.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+shellBody+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return script
	}

	// Windows: compile a Go binary that performs the same operation.
	src := `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: editor <file>")
		os.Exit(1)
	}
	path := os.Args[1]
	_ = json.Marshal // keep import live if goBody doesn't use it
` + goBody + `
}
`
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcFile, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "editor.exe")
	cmd := exec.Command("go", "build", "-o", bin, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build editor binary: %v\n%s", err, out)
	}
	return bin
}

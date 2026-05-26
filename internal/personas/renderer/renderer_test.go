package renderer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/personas/renderer"
)

// buildPersonaDir creates a temporary directory with a handful of fake persona
// files and returns its path.
func buildPersonaDir(t *testing.T, personas map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range personas {
		path := filepath.Join(dir, name+".md")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing persona fixture %s: %v", path, err)
		}
	}
	return dir
}

// samplePersona returns a minimal but realistic canonical persona document.
func samplePersona(name, description string) string {
	return `---
name: archigraph-` + name + `
description: >
  ` + description + `
tools: Read, Glob, mcp__archigraph__*
model: sonnet
---

## Role

You are the ` + name + ` persona.

## Steps

1. Call ` + "`archigraph_whoami`" + `.
2. Analyse the graph.
`
}

// ---------------------------------------------------------------------------
// Target: windsurf
// ---------------------------------------------------------------------------

func TestRenderWindsurf_OutputStructure(t *testing.T) {
	srcDir := buildPersonaDir(t, map[string]string{
		"architect":        samplePersona("architect", "Reviews internal system structure."),
		"security-auditor": samplePersona("security-auditor", "Audits security findings."),
	})
	outDir := t.TempDir()

	results, err := renderer.Render(srcDir, outDir, renderer.TargetWindsurf)
	if err != nil {
		t.Fatalf("Render(windsurf) error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		// Each dest must live under .windsurf/workflows/
		rel, err := filepath.Rel(outDir, r.Dest)
		if err != nil {
			t.Fatalf("filepath.Rel: %v", err)
		}
		wantPrefix := filepath.Join(".windsurf", "workflows")
		if !strings.HasPrefix(rel, wantPrefix) {
			t.Errorf("windsurf dest %q does not start with %q", rel, wantPrefix)
		}
		if !strings.HasSuffix(rel, ".md") {
			t.Errorf("windsurf dest %q does not end with .md", rel)
		}

		// File must exist and contain the generator comment.
		data, err := os.ReadFile(r.Dest)
		if err != nil {
			t.Fatalf("reading windsurf output %s: %v", r.Dest, err)
		}
		content := string(data)
		if !strings.Contains(content, "archigraph personas render --target windsurf") {
			t.Errorf("windsurf %s missing generator comment", r.Name)
		}
		if !strings.Contains(content, "description:") {
			t.Errorf("windsurf %s missing description field", r.Name)
		}
		// Body must be preserved.
		if !strings.Contains(content, "## Role") {
			t.Errorf("windsurf %s missing body content", r.Name)
		}
		// Original 'tools:' and 'model:' fields must NOT be re-emitted in the new frontmatter.
		// They appear only in the body pass-through, not in the Windsurf frontmatter block.
		fm, _ := extractFrontmatter(content)
		if strings.Contains(fm, "tools:") {
			t.Errorf("windsurf %s frontmatter must not contain 'tools:' field", r.Name)
		}
	}
}

func TestRenderWindsurf_Idempotent(t *testing.T) {
	srcDir := buildPersonaDir(t, map[string]string{
		"architect": samplePersona("architect", "Reviews internal system structure."),
	})
	outDir := t.TempDir()

	r1, err := renderer.Render(srcDir, outDir, renderer.TargetWindsurf)
	if err != nil || len(r1) != 1 {
		t.Fatalf("first render failed: %v, results=%d", err, len(r1))
	}
	data1, _ := os.ReadFile(r1[0].Dest)

	r2, err := renderer.Render(srcDir, outDir, renderer.TargetWindsurf)
	if err != nil || len(r2) != 1 {
		t.Fatalf("second render failed: %v", err)
	}
	data2, _ := os.ReadFile(r2[0].Dest)

	if string(data1) != string(data2) {
		t.Error("windsurf render is not idempotent: second run produced different output")
	}
}

// ---------------------------------------------------------------------------
// Target: cursor
// ---------------------------------------------------------------------------

func TestRenderCursor_OutputStructure(t *testing.T) {
	srcDir := buildPersonaDir(t, map[string]string{
		"refactor-critic": samplePersona("refactor-critic", "Critiques refactoring opportunities."),
		"data-engineer":   samplePersona("data-engineer", "Reviews data pipeline design."),
	})
	outDir := t.TempDir()

	results, err := renderer.Render(srcDir, outDir, renderer.TargetCursor)
	if err != nil {
		t.Fatalf("Render(cursor) error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		rel, err := filepath.Rel(outDir, r.Dest)
		if err != nil {
			t.Fatalf("filepath.Rel: %v", err)
		}
		wantPrefix := filepath.Join(".cursor", "commands")
		if !strings.HasPrefix(rel, wantPrefix) {
			t.Errorf("cursor dest %q does not start with %q", rel, wantPrefix)
		}
		if !strings.HasSuffix(rel, ".md") {
			t.Errorf("cursor dest %q does not end with .md", rel)
		}

		data, err := os.ReadFile(r.Dest)
		if err != nil {
			t.Fatalf("reading cursor output %s: %v", r.Dest, err)
		}
		content := string(data)
		if !strings.Contains(content, "archigraph personas render --target cursor") {
			t.Errorf("cursor %s missing generator comment", r.Name)
		}
		if !strings.Contains(content, "description:") {
			t.Errorf("cursor %s missing description field", r.Name)
		}
		if !strings.Contains(content, "## Role") {
			t.Errorf("cursor %s missing body content", r.Name)
		}
	}
}

func TestRenderCursor_Idempotent(t *testing.T) {
	srcDir := buildPersonaDir(t, map[string]string{
		"qa-reviewer": samplePersona("qa-reviewer", "Reviews test coverage."),
	})
	outDir := t.TempDir()

	r1, _ := renderer.Render(srcDir, outDir, renderer.TargetCursor)
	data1, _ := os.ReadFile(r1[0].Dest)

	r2, _ := renderer.Render(srcDir, outDir, renderer.TargetCursor)
	data2, _ := os.ReadFile(r2[0].Dest)

	if string(data1) != string(data2) {
		t.Error("cursor render is not idempotent")
	}
}

// ---------------------------------------------------------------------------
// Target: claude-code
// ---------------------------------------------------------------------------

func TestRenderClaudeCode_CopiesVerbatim(t *testing.T) {
	content := samplePersona("architect", "Reviews internal system structure.")
	srcDir := buildPersonaDir(t, map[string]string{"architect": content})
	outDir := t.TempDir()

	results, err := renderer.Render(srcDir, outDir, renderer.TargetClaudeCode)
	if err != nil {
		t.Fatalf("Render(claude-code) error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	data, _ := os.ReadFile(results[0].Dest)
	// Verbatim copy: content must match exactly (file ends with \n from samplePersona).
	if string(data) != content+"\n" && string(data) != content {
		// allow either trailing-newline variant
		got := string(data)
		want := content
		if strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
			t.Errorf("claude-code output differs from source:\ngot:  %q\nwant: %q", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Target: codex
// ---------------------------------------------------------------------------

func TestRenderCodex_StubContainsTODO(t *testing.T) {
	srcDir := buildPersonaDir(t, map[string]string{
		"performance-reviewer": samplePersona("performance-reviewer", "Reviews performance."),
	})
	outDir := t.TempDir()

	results, err := renderer.Render(srcDir, outDir, renderer.TargetCodex)
	if err != nil {
		t.Fatalf("Render(codex) error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	data, _ := os.ReadFile(results[0].Dest)
	content := string(data)
	if !strings.Contains(content, "TODO") {
		t.Error("codex stub missing TODO marker")
	}
	if !strings.HasSuffix(results[0].Dest, ".codex.md") {
		t.Errorf("codex file should end with .codex.md, got %s", results[0].Dest)
	}
}

// ---------------------------------------------------------------------------
// ParseTarget
// ---------------------------------------------------------------------------

func TestParseTarget_Valid(t *testing.T) {
	for _, s := range []string{"claude-code", "windsurf", "cursor", "codex"} {
		if _, err := renderer.ParseTarget(s); err != nil {
			t.Errorf("ParseTarget(%q) unexpected error: %v", s, err)
		}
	}
}

func TestParseTarget_Invalid(t *testing.T) {
	if _, err := renderer.ParseTarget("vscode"); err == nil {
		t.Error("ParseTarget(vscode) should return error")
	}
}

// ---------------------------------------------------------------------------
// splitFrontmatter (indirectly via output inspection)
// ---------------------------------------------------------------------------

// extractFrontmatter pulls the YAML block between the first and second --- from s.
func extractFrontmatter(s string) (fm, body string) {
	if !strings.HasPrefix(s, "---") {
		return "", s
	}
	rest := strings.TrimPrefix(s, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", s
	}
	return rest[:idx], rest[idx+4:]
}

// ---------------------------------------------------------------------------
// DiscoverPersonasDir
// ---------------------------------------------------------------------------

func TestDiscoverPersonasDir_Found(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "skills", "archigraph-consult", "personas")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := renderer.DiscoverPersonasDir(root, 4)
	if err != nil {
		t.Fatalf("DiscoverPersonasDir error: %v", err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

func TestDiscoverPersonasDir_NotFound(t *testing.T) {
	root := t.TempDir()
	_, err := renderer.DiscoverPersonasDir(root, 2)
	if err == nil {
		t.Error("expected error when personas dir not found")
	}
}

// ---------------------------------------------------------------------------
// Empty src dir
// ---------------------------------------------------------------------------

func TestRender_EmptySrcDir(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()
	results, err := renderer.Render(srcDir, outDir, renderer.TargetWindsurf)
	if err != nil {
		t.Fatalf("unexpected error on empty srcDir: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty srcDir, got %d", len(results))
	}
}

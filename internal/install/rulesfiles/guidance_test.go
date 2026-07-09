package rulesfiles

// guidance_test.go — #5702: the personal self-gating Claude guidance block
// (RenderPersonalBlock) and its idempotent, content-preserving writer
// (UpsertGuidance). Also guards that the repo-root CLAUDE.md is no longer a
// per-repo Target (guidance is personal now).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTargets_ExcludesRepoClaudeMd guards that the project-root CLAUDE.md is
// no longer swept by WriteAll/Scan — its guidance moved to the personal
// ~/.claude/CLAUDE.md (#5702).
func TestTargets_ExcludesRepoClaudeMd(t *testing.T) {
	for _, tgt := range Targets {
		if tgt == "CLAUDE.md" {
			t.Fatalf("Targets must not include repo-root CLAUDE.md anymore, got %v", Targets)
		}
	}
}

// TestRenderPersonalBlock_SelfGating verifies the personal block is generic
// (no group name), carries the shared markers, and states the self-gating
// pre-conditions so an agent ignores it unless grafel is connected + indexed.
func TestRenderPersonalBlock_SelfGating(t *testing.T) {
	b := RenderPersonalBlock()
	mustContain := []string{
		StartMarker,
		EndMarker,
		"self-gating",
		"MCP server is connected",
		"grafel_index_status",
		"ignore this section entirely",
		"auto-updated by `grafel install`",
	}
	for _, s := range mustContain {
		if !strings.Contains(b, s) {
			t.Errorf("personal block missing %q:\n%s", s, b)
		}
	}
	// It is GLOBAL, so it must NOT embed a group-name placeholder or a group.
	if strings.Contains(b, "<group-name>") || strings.Contains(b, "part of grafel group") {
		t.Errorf("personal block must be group-agnostic:\n%s", b)
	}
}

// TestUpsertGuidance_CreatesAndUpdatesInPlace verifies a fresh write creates
// the file with exactly one block, and a re-write replaces it in place
// (idempotent — never duplicated).
func TestUpsertGuidance_CreatesAndUpdatesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "CLAUDE.md")

	if err := UpsertGuidance(path, RenderPersonalBlock()); err != nil {
		t.Fatalf("first UpsertGuidance: %v", err)
	}
	first, _ := os.ReadFile(path)
	if n := strings.Count(string(first), "grafel:mcp-usage:start"); n != 1 {
		t.Fatalf("expected 1 block after create, got %d:\n%s", n, first)
	}

	// Second write must not duplicate.
	if err := UpsertGuidance(path, RenderPersonalBlock()); err != nil {
		t.Fatalf("second UpsertGuidance: %v", err)
	}
	second, _ := os.ReadFile(path)
	if n := strings.Count(string(second), "grafel:mcp-usage:start"); n != 1 {
		t.Fatalf("expected still 1 block after re-write, got %d:\n%s", n, second)
	}
}

// TestUpsertGuidance_PreservesSurroundingProse verifies UpsertGuidance never
// touches a user's own content: it appends after existing prose and, on
// re-write, replaces only the block while keeping the prose byte-for-byte.
func TestUpsertGuidance_PreservesSurroundingProse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	prose := "# My personal instructions\n\nAlways be concise.\n"
	if err := os.WriteFile(path, []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpsertGuidance(path, RenderPersonalBlock()); err != nil {
		t.Fatalf("UpsertGuidance: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Always be concise.") {
		t.Errorf("user prose lost:\n%s", data)
	}
	if !strings.Contains(string(data), StartMarker) {
		t.Errorf("block not appended:\n%s", data)
	}

	// Re-write: prose still intact, still one block.
	if err := UpsertGuidance(path, RenderPersonalBlock()); err != nil {
		t.Fatalf("re-write: %v", err)
	}
	data2, _ := os.ReadFile(path)
	if !strings.Contains(string(data2), "Always be concise.") {
		t.Errorf("user prose lost on re-write:\n%s", data2)
	}
	if n := strings.Count(string(data2), "grafel:mcp-usage:start"); n != 1 {
		t.Errorf("expected 1 block after re-write, got %d:\n%s", n, data2)
	}
}

// TestUpsertGuidance_NoStaleHeuristic proves UpsertGuidance does NOT apply the
// predecessor/stale-file logic: a personal file that merely mentions
// "archigraph" must still receive the block (WriteTargets would have skipped
// it as mixed-stale).
func TestUpsertGuidance_NoStaleHeuristic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	prose := "# Notes\n\nWe migrated off archigraph last year.\n"
	if err := os.WriteFile(path, []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertGuidance(path, RenderPersonalBlock()); err != nil {
		t.Fatalf("UpsertGuidance: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), StartMarker) {
		t.Errorf("block should have been written despite 'archigraph' mention:\n%s", data)
	}
	if !strings.Contains(string(data), "migrated off archigraph") {
		t.Errorf("user prose lost:\n%s", data)
	}
}

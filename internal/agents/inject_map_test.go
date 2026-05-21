package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time {
	return time.Date(2026, 5, 21, 9, 47, 0, 0, time.UTC)
}

func baseStats(group string) Stats {
	return Stats{
		Group:         group,
		DashboardPort: 47274,
		Entities:      500,
		Relationships: 1200,
		HTTPEndpoints: 42,
		ProcessFlows:  10,
		Queues:        3,
		Topics:        1,
		IndexedAt:     fixedTime(),
		BinaryPath:    "/usr/local/bin/archigraph",
	}
}

// TestRenderBlock_containsMarkers verifies the rendered block has both markers.
func TestRenderBlock_containsMarkers(t *testing.T) {
	block := renderBlock(baseStats("mygroup"), detectedTools{})
	if !strings.Contains(block, MapStartMarker) {
		t.Error("block missing start marker")
	}
	if !strings.Contains(block, MapEndMarker) {
		t.Error("block missing end marker")
	}
}

// TestRenderBlock_withGroup verifies group-scoped API paths appear when Group is set.
func TestRenderBlock_withGroup(t *testing.T) {
	block := renderBlock(baseStats("mygroup"), detectedTools{})
	if !strings.Contains(block, "/graph/mygroup") {
		t.Error("block missing dashboard path with group")
	}
	if !strings.Contains(block, "/paths/mygroup") {
		t.Error("block missing /paths/<group>")
	}
	if !strings.Contains(block, "/flows/mygroup") {
		t.Error("block missing /flows/<group>")
	}
	if !strings.Contains(block, "/topology/mygroup") {
		t.Error("block missing /topology/<group>")
	}
}

// TestRenderBlock_noGroup verifies the block renders without group-scoped paths.
func TestRenderBlock_noGroup(t *testing.T) {
	s := baseStats("")
	block := renderBlock(s, detectedTools{})
	if strings.Contains(block, "/graph/") {
		t.Error("block should not contain /graph/<group> when group is empty")
	}
	if !strings.Contains(block, "Entities") {
		t.Error("block missing Entities line")
	}
}

// TestRenderBlock_timestamp verifies the indexed-at timestamp is included.
func TestRenderBlock_timestamp(t *testing.T) {
	block := renderBlock(baseStats("g"), detectedTools{})
	if !strings.Contains(block, "2026-05-21 09:47 UTC") {
		t.Errorf("block missing timestamp; got:\n%s", block)
	}
}

// TestRenderBlock_claudeHint verifies Claude-specific hint when .claude/ detected.
func TestRenderBlock_claudeHint(t *testing.T) {
	block := renderBlock(baseStats("g"), detectedTools{claude: true})
	if !strings.Contains(block, "Claude Code") {
		t.Error("block missing Claude Code hint for detected .claude/ dir")
	}
}

// TestRenderBlock_cursorHint verifies Cursor-specific hint when .cursor/ detected.
func TestRenderBlock_cursorHint(t *testing.T) {
	block := renderBlock(baseStats("g"), detectedTools{cursor: true})
	if !strings.Contains(block, "Cursor") {
		t.Error("block missing Cursor hint for detected .cursor/ dir")
	}
}

// TestRenderBlock_binaryPath verifies the binary path appears in the MCP snippet.
func TestRenderBlock_binaryPath(t *testing.T) {
	block := renderBlock(baseStats("g"), detectedTools{})
	if !strings.Contains(block, "/usr/local/bin/archigraph") {
		t.Error("block missing binary path in MCP snippet")
	}
}

// TestUpsertFile_createsNewFile verifies that upsertFile creates AGENTS.md when missing.
func TestUpsertFile_createsNewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	block := renderBlock(baseStats("g"), detectedTools{})

	if err := upsertFile(p, block); err != nil {
		t.Fatalf("upsertFile: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), MapStartMarker) {
		t.Error("created file missing start marker")
	}
}

// TestUpsertFile_appendsToExisting verifies the block is appended when file has no markers.
func TestUpsertFile_appendsToExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	existing := "# My Project\n\nSome existing content.\n"
	if err := os.WriteFile(p, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	block := renderBlock(baseStats("g"), detectedTools{})
	if err := upsertFile(p, block); err != nil {
		t.Fatalf("upsertFile: %v", err)
	}

	data, _ := os.ReadFile(p)
	s := string(data)
	if !strings.HasPrefix(s, "# My Project") {
		t.Error("existing content should be preserved at top")
	}
	if !strings.Contains(s, MapStartMarker) {
		t.Error("marker block not appended")
	}
}

// TestUpsertFile_replacesExistingBlock verifies idempotent in-place update.
func TestUpsertFile_replacesExistingBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")

	// Write an initial block with stale timestamp.
	stale := baseStats("g")
	stale.IndexedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	stale.Entities = 100
	block1 := renderBlock(stale, detectedTools{})
	if err := os.WriteFile(p, []byte("# Header\n\n"+block1+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Now upsert with fresh stats.
	fresh := baseStats("g")
	fresh.Entities = 999
	block2 := renderBlock(fresh, detectedTools{})
	if err := upsertFile(p, block2); err != nil {
		t.Fatalf("upsertFile: %v", err)
	}

	data, _ := os.ReadFile(p)
	s := string(data)

	// Header must be intact.
	if !strings.HasPrefix(s, "# Header") {
		t.Error("header lost after upsert")
	}

	// Should contain new entity count, not old.
	if !strings.Contains(s, "999") {
		t.Error("updated entity count (999) not found")
	}
	if strings.Contains(s, "Entities**: 100") {
		t.Error("stale entity count (100) still present after update")
	}

	// Exactly one start marker.
	count := strings.Count(s, MapStartMarker)
	if count != 1 {
		t.Errorf("expected 1 start marker, got %d", count)
	}
}

// TestResolveTargetFile_prefersAGENTSMD verifies AGENTS.md wins over CLAUDE.md.
func TestResolveTargetFile_prefersAGENTSMD(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	got := resolveTargetFile(dir)
	if filepath.Base(got) != "AGENTS.md" {
		t.Errorf("expected AGENTS.md, got %s", filepath.Base(got))
	}
}

// TestResolveTargetFile_fallsBackToCLAUDE verifies CLAUDE.md is chosen when AGENTS.md absent.
func TestResolveTargetFile_fallsBackToCLAUDE(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0o644)
	got := resolveTargetFile(dir)
	if filepath.Base(got) != "CLAUDE.md" {
		t.Errorf("expected CLAUDE.md, got %s", filepath.Base(got))
	}
}

// TestResolveTargetFile_defaultsToAGENTSMD verifies default path when no file exists.
func TestResolveTargetFile_defaultsToAGENTSMD(t *testing.T) {
	dir := t.TempDir()
	got := resolveTargetFile(dir)
	if filepath.Base(got) != "AGENTS.md" {
		t.Errorf("expected AGENTS.md default, got %s", filepath.Base(got))
	}
}

// TestDetectAITools verifies .claude/ and .cursor/ detection.
func TestDetectAITools(t *testing.T) {
	dir := t.TempDir()

	// No tools.
	d := detectAITools(dir)
	if d.claude || d.cursor {
		t.Error("no tools should be detected in empty dir")
	}

	// Add .claude/
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	d = detectAITools(dir)
	if !d.claude {
		t.Error(".claude/ not detected")
	}
	if d.cursor {
		t.Error("cursor should not be detected")
	}

	// Add .cursor/
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755)
	d = detectAITools(dir)
	if !d.claude || !d.cursor {
		t.Error("both tools should be detected")
	}
}

// TestInjectArchitectureMap_endToEnd exercises the full public entrypoint.
func TestInjectArchitectureMap_endToEnd(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed a CLAUDE.md so that file is chosen over new AGENTS.md.
	claudePath := filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(claudePath, []byte("# Existing\n"), 0o644)

	s := baseStats("mygroup")
	if err := InjectArchitectureMap(dir, s); err != nil {
		t.Fatalf("InjectArchitectureMap: %v", err)
	}

	// CLAUDE.md should now have the block.
	data, _ := os.ReadFile(claudePath)
	if !strings.Contains(string(data), MapStartMarker) {
		t.Error("CLAUDE.md missing architecture map block")
	}
	// AGENTS.md should NOT have been created since CLAUDE.md existed.
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Error("AGENTS.md should not have been created when CLAUDE.md exists")
	}
}

// TestInjectArchitectureMap_createsAGENTSMD verifies file creation when none exists.
func TestInjectArchitectureMap_createsAGENTSMD(t *testing.T) {
	dir := t.TempDir()
	s := baseStats("g")
	if err := InjectArchitectureMap(dir, s); err != nil {
		t.Fatalf("InjectArchitectureMap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Error("AGENTS.md should have been created")
	}
}

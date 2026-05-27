package rulesfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteAll_FreshRepo verifies that a brand-new repo gets every
// target rules file populated with the canonical block.
func TestWriteAll_FreshRepo(t *testing.T) {
	repo := t.TempDir()
	var logger bytes.Buffer

	res, err := WriteAll(repo, WriteOptions{GroupName: "demo", Logger: &logger})
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}

	if len(res.Written) != len(Targets) {
		t.Fatalf("expected %d targets written, got %d (%v)", len(Targets), len(res.Written), res.Written)
	}

	for _, target := range Targets {
		path := filepath.Join(repo, target)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing target %s: %v", target, err)
		}
		if !bytes.Contains(data, []byte(StartMarker)) {
			t.Errorf("%s: start marker not found", target)
		}
		if !bytes.Contains(data, []byte(EndMarker)) {
			t.Errorf("%s: end marker not found", target)
		}
		if !bytes.Contains(data, []byte("archigraph MCP")) {
			t.Errorf("%s: block payload not found", target)
		}
		if !bytes.Contains(data, []byte("**demo**")) {
			t.Errorf("%s: group name not embedded", target)
		}
	}
}

// TestWriteAll_Idempotent verifies that a second run does not duplicate
// the block.
func TestWriteAll_Idempotent(t *testing.T) {
	repo := t.TempDir()
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("first WriteAll: %v", err)
	}
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("second WriteAll: %v", err)
	}

	for _, target := range Targets {
		path := filepath.Join(repo, target)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", target, err)
		}
		count := bytes.Count(data, []byte(StartMarker))
		if count != 1 {
			t.Errorf("%s: expected exactly one block, found %d", target, count)
		}
	}
}

// TestWriteAll_ReplacesOlderVersionBlock verifies that a block with an
// older version marker is replaced in-place.
func TestWriteAll_ReplacesOlderVersionBlock(t *testing.T) {
	repo := t.TempDir()
	oldBlock := "<!-- archigraph:mcp-usage:start v=0 -->\nstale\n<!-- archigraph:mcp-usage:end -->\n"
	if err := os.WriteFile(filepath.Join(repo, ".windsurfrules"), []byte(oldBlock), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".windsurfrules"))
	if bytes.Contains(data, []byte("v=0")) {
		t.Errorf("old version marker still present: %s", data)
	}
	if !bytes.Contains(data, []byte("v=1")) {
		t.Errorf("new version marker missing: %s", data)
	}
}

// TestWriteAll_PreservesUnrelatedContent ensures that pre-existing
// content with no archigraph block and no predecessor refs is preserved
// and the block is appended.
func TestWriteAll_PreservesUnrelatedContent(t *testing.T) {
	repo := t.TempDir()
	original := "# My Project\n\nLocal notes that mention nothing relevant.\n"
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if !bytes.Contains(data, []byte("# My Project")) {
		t.Errorf("original content lost: %s", data)
	}
	if !bytes.Contains(data, []byte(StartMarker)) {
		t.Errorf("block not appended")
	}
}

// TestWriteAll_PureStaleGraphifyOverwritten covers the heuristic for
// "the whole file is predecessor content" — should be overwritten and
// a log line emitted.
func TestWriteAll_PureStaleGraphifyOverwritten(t *testing.T) {
	repo := t.TempDir()
	stale := "# Graphify\n\n- Run `graphify update` to refresh the graph\n- See graphify-out/GRAPH_REPORT.md\n"
	if err := os.WriteFile(filepath.Join(repo, ".windsurfrules"), []byte(stale), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var logger bytes.Buffer
	res, err := WriteAll(repo, WriteOptions{GroupName: "demo", Logger: &logger})
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if len(res.ReplacedStale) == 0 {
		t.Fatalf("expected ReplacedStale, got %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".windsurfrules"))
	if bytes.Contains(data, []byte("graphify")) {
		t.Errorf("graphify content not removed: %s", data)
	}
	if !strings.Contains(logger.String(), "replaced stale graphify content") {
		t.Errorf("expected log line, got: %s", logger.String())
	}
}

// TestWriteAll_MixedStaleSkippedWithWarning covers the case where a
// file mentions graphify alongside unrelated content — should NOT be
// overwritten; a warning is emitted.
func TestWriteAll_MixedStaleSkippedWithWarning(t *testing.T) {
	repo := t.TempDir()
	mixed := "# Engineering Handbook\n\n" +
		"This repo is the canonical source of truth for our payments API.\n" +
		"It exposes the /v2/charges endpoint and consumes the orders.created topic.\n" +
		"Historical note: an older indexing tool called graphify was used here.\n" +
		"Please use the on-call rota in PagerDuty for incidents.\n" +
		"Refer to docs/architecture.md for the full system design.\n" +
		"Refer to docs/runbooks for operational procedures.\n" +
		"Owners are listed in CODEOWNERS at the repo root.\n"
	if err := os.WriteFile(filepath.Join(repo, ".windsurfrules"), []byte(mixed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var logger bytes.Buffer
	res, err := WriteAll(repo, WriteOptions{GroupName: "demo", Logger: &logger})
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if len(res.SkippedMixedStale) == 0 {
		t.Fatalf("expected SkippedMixedStale, got %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".windsurfrules"))
	if !bytes.Contains(data, []byte("payments API")) {
		t.Errorf("user content lost: %s", data)
	}
	if bytes.Contains(data, []byte(StartMarker)) {
		t.Errorf("block was written despite mixed-stale; should have been skipped")
	}
	if !strings.Contains(logger.String(), "please migrate manually") {
		t.Errorf("expected warning, got: %s", logger.String())
	}
}

// TestScan_StatusesAcrossFileShapes covers MISSING / STALE / OUTDATED /
// OK in a single repo.
func TestScan_StatusesAcrossFileShapes(t *testing.T) {
	repo := t.TempDir()

	// AGENTS.md → OK (current block).
	current := RenderBlock("demo") + "\n"
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(current), 0o644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}

	// CLAUDE.md → OUTDATED (older version).
	old := "<!-- archigraph:mcp-usage:start v=0 -->\nbody\n<!-- archigraph:mcp-usage:end -->\n"
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(old), 0o644); err != nil {
		t.Fatalf("seed CLAUDE.md: %v", err)
	}

	// .windsurfrules → STALE (graphify content, no block).
	stale := "# Graphify\nRun `graphify update`\n"
	if err := os.WriteFile(filepath.Join(repo, ".windsurfrules"), []byte(stale), 0o644); err != nil {
		t.Fatalf("seed .windsurfrules: %v", err)
	}

	// .cursorrules, .codeium/instructions.md, .github/copilot-instructions.md → MISSING.

	statuses := Scan(repo)
	byTarget := map[string]FileStatus{}
	for _, s := range statuses {
		byTarget[s.Target] = s
	}

	if got := byTarget["AGENTS.md"].Status; got != StatusOK {
		t.Errorf("AGENTS.md: expected OK, got %s", got)
	}
	if got := byTarget["CLAUDE.md"].Status; got != StatusOutdated {
		t.Errorf("CLAUDE.md: expected OUTDATED, got %s", got)
	}
	if got := byTarget[".windsurfrules"].Status; got != StatusStale {
		t.Errorf(".windsurfrules: expected STALE, got %s", got)
	}
	if got := byTarget[".cursorrules"].Status; got != StatusMissing {
		t.Errorf(".cursorrules: expected MISSING, got %s", got)
	}
	if got := byTarget[".codeium/instructions.md"].Status; got != StatusMissing {
		t.Errorf(".codeium/instructions.md: expected MISSING, got %s", got)
	}
	if got := byTarget[".github/copilot-instructions.md"].Status; got != StatusMissing {
		t.Errorf(".github/copilot-instructions.md: expected MISSING, got %s", got)
	}
}

// TestIsPureStaleFile checks the short-pure-stale heuristic.
func TestIsPureStaleFile(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"pure stale", "# Graphify\n\n- Run `graphify update`\n- See graphify-out/GRAPH_REPORT.md\n", true},
		{"mixed", "# Project\n\nWe use graphify here.\nAlso unrelated note about payments.\n", false},
		{"too long", strings.Repeat("graphify line\n", 40), false},
		{"empty", "", true}, // vacuously pure
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPureStaleFile([]byte(tc.in))
			if got != tc.want {
				t.Errorf("isPureStaleFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

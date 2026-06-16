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
		if !bytes.Contains(data, []byte("grafel MCP")) {
			t.Errorf("%s: block payload not found", target)
		}
		if !bytes.Contains(data, []byte("**demo**")) {
			t.Errorf("%s: group name not embedded", target)
		}
		// The imperative STANDING DIRECTIVE (#3648) must be present in
		// every target so agents keep using grafel for the whole
		// session instead of drifting back to grep. Assert the key phrases
		// so this guard can't silently rot if the block is reworded.
		if !bytes.Contains(data, []byte("STANDING DIRECTIVE")) {
			t.Errorf("%s: standing directive heading missing", target)
		}
		if !bytes.Contains(data, []byte("STRUCTURAL questions")) {
			t.Errorf("%s: directive does not mention STRUCTURAL questions", target)
		}
		if !bytes.Contains(data, []byte("not** `grep`")) {
			t.Errorf("%s: directive does not push back against grep", target)
		}
		if !bytes.Contains(data, []byte("grafel_find")) {
			t.Errorf("%s: directive does not name grafel_find", target)
		}
		if !bytes.Contains(data, []byte("WHOLE session")) {
			t.Errorf("%s: directive does not assert whole-session scope", target)
		}
	}
}

// TestWriteTargets_KiroAndAntigravity verifies the #5255 nested-directory
// targets (Kiro steering + Antigravity rules) round-trip: WriteTargets
// creates the parent dirs and block, a second write is idempotent, and
// RemoveTargets deletes the grafel-authored files.
func TestWriteTargets_KiroAndAntigravity(t *testing.T) {
	repo := t.TempDir()
	targets := []string{".kiro/steering/grafel.md", ".agent/rules/grafel.md"}

	res, err := WriteTargets(repo, WriteOptions{GroupName: "demo"}, targets)
	if err != nil {
		t.Fatalf("WriteTargets: %v", err)
	}
	if len(res.Written) != 2 {
		t.Fatalf("expected 2 written, got %v", res.Written)
	}
	for _, target := range targets {
		data, err := os.ReadFile(filepath.Join(repo, target))
		if err != nil {
			t.Fatalf("missing %s: %v", target, err)
		}
		if !bytes.Contains(data, []byte(StartMarker)) || !bytes.Contains(data, []byte("grafel MCP")) {
			t.Errorf("%s: block not written", target)
		}
	}

	// Idempotent: second write does not duplicate the block.
	if _, err := WriteTargets(repo, WriteOptions{GroupName: "demo"}, targets); err != nil {
		t.Fatalf("WriteTargets second: %v", err)
	}
	for _, target := range targets {
		data, _ := os.ReadFile(filepath.Join(repo, target))
		if n := bytes.Count(data, []byte(StartMarker)); n != 1 {
			t.Errorf("%s: expected 1 block, got %d", target, n)
		}
	}

	// Remove strips/deletes grafel-authored files.
	rr, err := RemoveTargets(repo, targets)
	if err != nil {
		t.Fatalf("RemoveTargets: %v", err)
	}
	if len(rr.Deleted) != 2 {
		t.Fatalf("expected 2 deleted (grafel sole author), got %v / stripped %v", rr.Deleted, rr.Stripped)
	}
	for _, target := range targets {
		if _, err := os.Stat(filepath.Join(repo, target)); !os.IsNotExist(err) {
			t.Errorf("%s: expected removed, stat err=%v", target, err)
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
	oldBlock := "<!-- grafel:mcp-usage:start v=0 -->\nstale\n<!-- grafel:mcp-usage:end -->\n"
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
	if !bytes.Contains(data, []byte(StartMarker)) {
		t.Errorf("current version marker missing: %s", data)
	}
	// The replaced block must carry the new directive, not just a bumped
	// version number.
	if !bytes.Contains(data, []byte("STANDING DIRECTIVE")) {
		t.Errorf("replaced block missing standing directive: %s", data)
	}
}

// TestWriteAll_PreservesUnrelatedContent ensures that pre-existing
// content with no grafel block and no predecessor refs is preserved
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
	old := "<!-- grafel:mcp-usage:start v=0 -->\nbody\n<!-- grafel:mcp-usage:end -->\n"
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

// TestWriteAll_ReplacesLegacyArchigraphBlock covers #5274: a file holding
// a legacy `archigraph:mcp-usage` block must be recognised and replaced
// in place by the current grafel block — no duplicate block, no leftover
// archigraph marker.
func TestWriteAll_ReplacesLegacyArchigraphBlock(t *testing.T) {
	repo := t.TempDir()
	legacy := "# Notes\n\n<!-- archigraph:mcp-usage:start v=1 -->\n## archigraph MCP\nold guidance pointing at archigraph\n<!-- archigraph:mcp-usage:end -->\n"
	path := filepath.Join(repo, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	// Legacy markers must be gone entirely.
	if strings.Contains(s, "archigraph:mcp-usage") {
		t.Errorf("legacy archigraph marker still present:\n%s", s)
	}
	// Exactly one current grafel block (no duplicate appended).
	if n := strings.Count(s, "grafel:mcp-usage:start"); n != 1 {
		t.Errorf("expected exactly 1 grafel start marker, got %d:\n%s", n, s)
	}
	if !strings.Contains(s, StartMarker) {
		t.Errorf("current grafel block missing:\n%s", s)
	}
	// Surrounding user content preserved.
	if !strings.Contains(s, "# Notes") {
		t.Errorf("surrounding user content lost:\n%s", s)
	}
}

// TestWriteAll_LegacyArchigraphOnlyFile verifies a file that is ONLY a
// legacy archigraph block is cleanly replaced (still one block, no dup).
func TestWriteAll_LegacyArchigraphOnlyFile(t *testing.T) {
	repo := t.TempDir()
	legacy := "<!-- archigraph:mcp-usage:start v=2 -->\nguidance\n<!-- archigraph:mcp-usage:end -->\n"
	path := filepath.Join(repo, ".cursorrules")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	s, _ := os.ReadFile(path)
	if strings.Contains(string(s), "archigraph:mcp-usage") {
		t.Errorf("legacy marker still present:\n%s", s)
	}
	if n := strings.Count(string(s), "grafel:mcp-usage:start"); n != 1 {
		t.Errorf("expected 1 grafel block, got %d:\n%s", n, s)
	}
}

// TestWriteAll_GraphifyStillHandled is a regression guard that the older
// graphify pure-stale heuristic still works after the archigraph changes.
func TestWriteAll_GraphifyStillHandled(t *testing.T) {
	repo := t.TempDir()
	stale := "# Graphify\n\n- Run `graphify update`\n"
	path := filepath.Join(repo, ".windsurfrules")
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := WriteAll(repo, WriteOptions{GroupName: "demo"})
	if err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if len(res.ReplacedStale) == 0 {
		t.Fatalf("graphify pure-stale not replaced: %+v", res)
	}
	s, _ := os.ReadFile(path)
	if strings.Contains(string(s), "graphify") {
		t.Errorf("graphify content not removed:\n%s", s)
	}
}

// TestRemoveAll_StripsBlockPreservesProse covers the uninstall side of
// #5274: stripping the grafel block must leave surrounding user prose
// intact, while a file that is ONLY the grafel block is deleted.
func TestRemoveAll_StripsBlockPreservesProse(t *testing.T) {
	repo := t.TempDir()

	// File with prose + grafel block → block stripped, prose kept.
	mixedPath := filepath.Join(repo, "CLAUDE.md")
	prose := "# My Project\n\nImportant local instructions.\n"
	if _, err := WriteAll(repo, WriteOptions{GroupName: "demo"}); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	// Overwrite CLAUDE.md with prose + block so we control the prose.
	mixed := prose + "\n" + RenderBlock("demo") + "\n"
	if err := os.WriteFile(mixedPath, []byte(mixed), 0o644); err != nil {
		t.Fatalf("seed mixed: %v", err)
	}

	res, err := RemoveAll(repo)
	if err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	// CLAUDE.md keeps prose, loses the block.
	data, rerr := os.ReadFile(mixedPath)
	if rerr != nil {
		t.Fatalf("CLAUDE.md should still exist: %v", rerr)
	}
	if strings.Contains(string(data), "grafel:mcp-usage") {
		t.Errorf("grafel block not stripped from mixed file:\n%s", data)
	}
	if !strings.Contains(string(data), "Important local instructions.") {
		t.Errorf("user prose lost:\n%s", data)
	}
	if !contains(res.Stripped, "CLAUDE.md") {
		t.Errorf("CLAUDE.md not reported stripped: %+v", res)
	}

	// Files that were ONLY the grafel block (every other Target, since
	// WriteAll wrote a block-only file there) are deleted.
	if _, err := os.Stat(filepath.Join(repo, ".windsurfrules")); !os.IsNotExist(err) {
		t.Errorf(".windsurfrules (block-only) should have been deleted, err=%v", err)
	}
	if !contains(res.Deleted, ".windsurfrules") {
		t.Errorf(".windsurfrules not reported deleted: %+v", res)
	}
}

// TestRemoveAll_LeavesNonGrafelFilesUntouched ensures removal never
// touches a file that has no grafel block.
func TestRemoveAll_LeavesNonGrafelFilesUntouched(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "CLAUDE.md")
	original := "# Just my notes\n\nNo grafel here.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := RemoveAll(repo)
	if err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("non-grafel file modified:\nwant %q\ngot  %q", original, data)
	}
	if len(res.Stripped) != 0 || len(res.Deleted) != 0 {
		t.Errorf("expected no-op, got %+v", res)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

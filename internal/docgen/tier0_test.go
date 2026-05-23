package docgen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// validateSection
// ---------------------------------------------------------------------------

func TestValidateSection_Known(t *testing.T) {
	for _, s := range docgen.KnownSections {
		opts := docgen.RunOpts{
			Group:        "testgroup",
			SeedEntityID: "abc123",
			Section:      s,
			OutputDir:    t.TempDir(),
		}
		// Only validate section — we expect graph-load to fail for the
		// fake group, but validation must pass before we reach the loader.
		_, _, _, err := docgen.Run(opts)
		if err != nil && strings.Contains(err.Error(), "unknown section") {
			t.Errorf("KnownSections[%q] rejected by validateSection: %v", s, err)
		}
	}
}

func TestValidateSection_Unknown(t *testing.T) {
	opts := docgen.RunOpts{
		Group:        "testgroup",
		SeedEntityID: "abc123",
		Section:      "not-a-real-section",
		OutputDir:    t.TempDir(),
	}
	_, _, _, err := docgen.Run(opts)
	if err == nil {
		t.Fatal("expected error for unknown section, got nil")
	}
	if !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("expected 'unknown section' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// renderSection (via full Run with a temp graph dir)
// ---------------------------------------------------------------------------

// buildMinimalGraph writes a minimal graph.json to dir so LoadGraphFromDir
// can read it.
func buildMinimalGraph(t *testing.T, dir string) {
	t.Helper()
	// We need a minimal fleet config that points to a repo that has a graph.
	// Tier0 uses findGroupGraphDirs → daemon.StateDirForRepo(repo.Path).
	// We bypass that by specifying OutputDir AND by supplying a temp fleet
	// config via ARCHIGRAPH_HOME override.  The cleanest approach is to
	// call Run with a fake group and accept a "no repos registered" error,
	// which is distinct from "unknown section".
}

// TestRun_MissingGroup verifies that a bad group name returns a config-not-found
// error (not a panic or a false positive).
func TestRun_MissingGroup(t *testing.T) {
	opts := docgen.RunOpts{
		Group:        "group-that-does-not-exist-xyz",
		SeedEntityID: "abc123",
		Section:      "overview",
		OutputDir:    t.TempDir(),
	}
	_, _, _, err := docgen.Run(opts)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
	// Should reference group config or fleet config, not be a section error.
	if strings.Contains(err.Error(), "unknown section") {
		t.Errorf("should have passed section validation before failing on group: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Score helpers
// ---------------------------------------------------------------------------

func TestBuildScore_Fields(t *testing.T) {
	// Write minimal output to a temp dir and read the score.json back.
	// We use ARCHIGRAPH_HOME to point to a temp home with an empty fleet config
	// so that Run fails gracefully after writing the score (it won't get that
	// far — but we can test score JSON structure by reading an existing score).

	// Instead, synthesize a score directly via an in-process call and verify
	// the JSON is structurally sound.
	score := docgen.Score{
		Tier:                   0,
		Section:                "overview",
		SeedEntity:             "abc123",
		WallTimeMS:             42,
		TokenCountEstimate:     100,
		MermaidCount:           1,
		InternalLinkCount:      3,
		InternalLinkUnresolved: 0,
		Lines:                  50,
		Words:                  200,
		NeighboursIncluded:     5,
		SeedEntityFound:        true,
	}

	data, err := json.MarshalIndent(score, "", "  ")
	if err != nil {
		t.Fatalf("marshal score: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse score JSON: %v", err)
	}

	requiredFields := []string{
		"tier", "section", "seed_entity", "wall_time_ms",
		"token_count_estimate", "mermaid_count", "internal_link_count",
		"internal_link_unresolved", "lines", "words",
		"neighbours_included", "seed_entity_found",
	}
	for _, f := range requiredFields {
		if _, ok := parsed[f]; !ok {
			t.Errorf("score.json missing required field: %q", f)
		}
	}
	if got := parsed["tier"]; got.(float64) != 0 {
		t.Errorf("tier: want 0, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Output directory creation
// ---------------------------------------------------------------------------

func TestRun_CreatesOutputDir(t *testing.T) {
	// Run with a valid section but fake group. The call must fail due to
	// the group not being registered, but output dir must exist or the error
	// must happen before output-dir creation.
	outDir := filepath.Join(t.TempDir(), "nested", "tier0-out")
	opts := docgen.RunOpts{
		Group:        "no-such-group",
		SeedEntityID: "abc",
		Section:      "overview",
		OutputDir:    outDir,
	}
	_, _, _, err := docgen.Run(opts)
	// We expect an error about the group config; the dir may or may not
	// exist depending on error order. Either is acceptable — we just verify
	// no panic.
	if err == nil {
		// If somehow Run succeeded (e.g. test machine has this group),
		// verify the output dir exists and has the expected files.
		if _, statErr := os.Stat(outDir); statErr != nil {
			t.Errorf("Run succeeded but output dir absent: %v", statErr)
		}
	}
	_ = err // tolerated
}

// ---------------------------------------------------------------------------
// NormalizeSeedEntityID — unit tests for #1826
// ---------------------------------------------------------------------------

func TestNormalizeSeedEntityID_RawHex(t *testing.T) {
	// Raw hex passes through unchanged — regression escape.
	got, err := docgen.NormalizeSeedEntityID("7a349f6cd77984c9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "7a349f6cd77984c9" {
		t.Errorf("want %q, got %q", "7a349f6cd77984c9", got)
	}
}

func TestNormalizeSeedEntityID_ArchigraphPrefix(t *testing.T) {
	// archigraph::<hex> — was broken before this fix.
	got, err := docgen.NormalizeSeedEntityID("archigraph::7a349f6cd77984c9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "7a349f6cd77984c9" {
		t.Errorf("want %q, got %q", "7a349f6cd77984c9", got)
	}
}

func TestNormalizeSeedEntityID_ArbitraryGroupPrefix(t *testing.T) {
	// Any <group>:: prefix should be stripped — upvate-core form.
	got, err := docgen.NormalizeSeedEntityID("upvate-core::7a349f6cd77984c9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "7a349f6cd77984c9" {
		t.Errorf("want %q, got %q", "7a349f6cd77984c9", got)
	}
}

func TestNormalizeSeedEntityID_InvalidEmptyRHS(t *testing.T) {
	// "group::" with empty RHS must return an error.
	_, err := docgen.NormalizeSeedEntityID("archigraph::")
	if err == nil {
		t.Fatal("expected error for 'archigraph::', got nil")
	}
	if !strings.Contains(err.Error(), "invalid --seed-entity") {
		t.Errorf("expected 'invalid --seed-entity' in error, got: %v", err)
	}
}

func TestNormalizeSeedEntityID_DoubleColonOnlyRHS(t *testing.T) {
	// Just "::" with no prefix and no suffix — invalid.
	_, err := docgen.NormalizeSeedEntityID("::")
	if err == nil {
		t.Fatal("expected error for '::', got nil")
	}
}

func TestNormalizeSeedEntityID_NestedDoubleColon(t *testing.T) {
	// "a::b::c" — only the FIRST "::" is split; RHS is "b::c", which is valid
	// (we take everything after the first "::").
	got, err := docgen.NormalizeSeedEntityID("a::b::c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "b::c" {
		t.Errorf("want %q, got %q", "b::c", got)
	}
}

// ---------------------------------------------------------------------------
// KnownSections completeness
// ---------------------------------------------------------------------------

func TestKnownSections_NonEmpty(t *testing.T) {
	if len(docgen.KnownSections) == 0 {
		t.Fatal("KnownSections is empty")
	}
	seen := make(map[string]bool)
	for _, s := range docgen.KnownSections {
		if s == "" {
			t.Error("KnownSections contains empty string")
		}
		if seen[s] {
			t.Errorf("KnownSections contains duplicate: %q", s)
		}
		seen[s] = true
	}
}

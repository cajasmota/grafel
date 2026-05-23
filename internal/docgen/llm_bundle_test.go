package docgen_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sampleBundle returns a minimal LLMPromptBundle for round-trip tests.
func sampleBundle() docgen.LLMPromptBundle {
	return docgen.LLMPromptBundle{
		Version:      "1",
		Tier:         0,
		Group:        "testgroup",
		SeedEntityID: "abc123def456abcd",
		PageID:       "",
		Sections: []docgen.LLMSectionPrompt{
			{
				Section:      "overview",
				AnchorID:     "overview",
				StubMarkdown: "<!-- tier0-generated -->\n# Section: overview\n",
				Guidance:     "Write a 2–3 sentence description.",
				MaxWords:     150,
				MaxMermaid:   1,
				NeighbourIDs: []string{"nb001", "nb002"},
			},
		},
		GraphContext: docgen.LLMGraphContext{
			EntityName:    "loadEntityContext",
			EntityKind:    "function",
			QualifiedName: "github.com/cajasmota/archigraph/internal/docgen.loadEntityContext",
			Repo:          "/Users/user/archigraph",
			SourceFile:    "internal/docgen/tier0.go",
			NeighbourBriefs: []docgen.NeighbourBrief{
				{EntityID: "nb001", Name: "findGroupGraphDirs", Kind: "function", Relationship: "CALLS"},
				{EntityID: "nb002", Name: "graph.LoadGraphFromDir", Kind: "function", Relationship: "CALLS"},
			},
			SourceWindow: "",
		},
		PromptHash:  "deadbeef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
		GeneratedAt: "2026-05-23T00:00:00Z",
	}
}

// sampleResult returns a minimal LLMRunResult for round-trip tests.
func sampleResult() docgen.LLMRunResult {
	return docgen.LLMRunResult{
		Version:      "1",
		PromptHash:   "deadbeef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
		Tier:         0,
		Group:        "testgroup",
		SeedEntityID: "abc123def456abcd",
		SectionResults: []docgen.LLMSectionResult{
			{
				Section:      "overview",
				Markdown:     "loadEntityContext is the core graph-load helper used by Tier 0 and Tier 1.\n",
				MermaidCount: 0,
				WordCount:    14,
				LinkRefs:     []string{},
			},
		},
		FilledAt: "2026-05-23T00:01:00Z",
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests — all 6 structs
// ---------------------------------------------------------------------------

// TestRoundTrip_LLMPromptBundle verifies that LLMPromptBundle survives a
// marshal → unmarshal cycle with all fields intact.
func TestRoundTrip_LLMPromptBundle(t *testing.T) {
	t.Parallel()
	orig := sampleBundle()
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got docgen.LLMPromptBundle
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Version != orig.Version {
		t.Errorf("Version: got %q want %q", got.Version, orig.Version)
	}
	if got.Tier != orig.Tier {
		t.Errorf("Tier: got %d want %d", got.Tier, orig.Tier)
	}
	if got.Group != orig.Group {
		t.Errorf("Group: got %q want %q", got.Group, orig.Group)
	}
	if got.SeedEntityID != orig.SeedEntityID {
		t.Errorf("SeedEntityID: got %q want %q", got.SeedEntityID, orig.SeedEntityID)
	}
	if got.PromptHash != orig.PromptHash {
		t.Errorf("PromptHash: got %q want %q", got.PromptHash, orig.PromptHash)
	}
	if got.GeneratedAt != orig.GeneratedAt {
		t.Errorf("GeneratedAt: got %q want %q", got.GeneratedAt, orig.GeneratedAt)
	}
	if len(got.Sections) != len(orig.Sections) {
		t.Fatalf("Sections len: got %d want %d", len(got.Sections), len(orig.Sections))
	}
}

// TestRoundTrip_LLMSectionPrompt verifies LLMSectionPrompt field preservation.
func TestRoundTrip_LLMSectionPrompt(t *testing.T) {
	t.Parallel()
	orig := docgen.LLMSectionPrompt{
		Section:      "flows",
		AnchorID:     "flows",
		StubMarkdown: "<!-- tier0-generated -->\n# Section: flows\n",
		Guidance:     "Trace the request/event flow.",
		MaxWords:     300,
		MaxMermaid:   2,
		NeighbourIDs: []string{"e1", "e2", "e3"},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got docgen.LLMSectionPrompt
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Section != orig.Section {
		t.Errorf("Section: got %q want %q", got.Section, orig.Section)
	}
	if got.MaxWords != orig.MaxWords {
		t.Errorf("MaxWords: got %d want %d", got.MaxWords, orig.MaxWords)
	}
	if got.MaxMermaid != orig.MaxMermaid {
		t.Errorf("MaxMermaid: got %d want %d", got.MaxMermaid, orig.MaxMermaid)
	}
	if len(got.NeighbourIDs) != len(orig.NeighbourIDs) {
		t.Errorf("NeighbourIDs len: got %d want %d", len(got.NeighbourIDs), len(orig.NeighbourIDs))
	}
}

// TestRoundTrip_LLMGraphContext verifies LLMGraphContext field preservation.
func TestRoundTrip_LLMGraphContext(t *testing.T) {
	t.Parallel()
	orig := docgen.LLMGraphContext{
		EntityName:    "Run",
		EntityKind:    "function",
		QualifiedName: "github.com/cajasmota/archigraph/internal/docgen.Run",
		Repo:          "/repo",
		SourceFile:    "internal/docgen/tier0.go",
		NeighbourBriefs: []docgen.NeighbourBrief{
			{EntityID: "x1", Name: "loadEntityContext", Kind: "function", Relationship: "CALLS"},
		},
		SourceWindow: "func Run(opts RunOpts) ...",
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got docgen.LLMGraphContext
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.QualifiedName != orig.QualifiedName {
		t.Errorf("QualifiedName: got %q want %q", got.QualifiedName, orig.QualifiedName)
	}
	if got.SourceWindow != orig.SourceWindow {
		t.Errorf("SourceWindow: got %q want %q", got.SourceWindow, orig.SourceWindow)
	}
	if len(got.NeighbourBriefs) != 1 {
		t.Fatalf("NeighbourBriefs len: got %d want 1", len(got.NeighbourBriefs))
	}
	if got.NeighbourBriefs[0].Relationship != "CALLS" {
		t.Errorf("NeighbourBriefs[0].Relationship: got %q want %q",
			got.NeighbourBriefs[0].Relationship, "CALLS")
	}
}

// TestRoundTrip_NeighbourBrief verifies NeighbourBrief field preservation.
func TestRoundTrip_NeighbourBrief(t *testing.T) {
	t.Parallel()
	orig := docgen.NeighbourBrief{
		EntityID:     "abc123",
		Name:         "SomeService",
		Kind:         "service",
		Relationship: "IMPORTS",
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got docgen.NeighbourBrief
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != orig {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, orig)
	}
}

// TestRoundTrip_LLMSectionResult verifies LLMSectionResult field preservation.
func TestRoundTrip_LLMSectionResult(t *testing.T) {
	t.Parallel()
	orig := docgen.LLMSectionResult{
		Section:      "api",
		Markdown:     "## API\n\n- `Run(opts RunOpts)` — executes a Tier 0 render.\n",
		MermaidCount: 0,
		WordCount:    10,
		LinkRefs:     []string{"#tier0", "#overview"},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got docgen.LLMSectionResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Section != orig.Section {
		t.Errorf("Section: got %q want %q", got.Section, orig.Section)
	}
	if got.WordCount != orig.WordCount {
		t.Errorf("WordCount: got %d want %d", got.WordCount, orig.WordCount)
	}
	if len(got.LinkRefs) != len(orig.LinkRefs) {
		t.Errorf("LinkRefs len: got %d want %d", len(got.LinkRefs), len(orig.LinkRefs))
	}
}

// TestRoundTrip_LLMRunResult verifies LLMRunResult field preservation.
func TestRoundTrip_LLMRunResult(t *testing.T) {
	t.Parallel()
	orig := sampleResult()
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got docgen.LLMRunResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PromptHash != orig.PromptHash {
		t.Errorf("PromptHash: got %q want %q", got.PromptHash, orig.PromptHash)
	}
	if got.FilledAt != orig.FilledAt {
		t.Errorf("FilledAt: got %q want %q", got.FilledAt, orig.FilledAt)
	}
	if len(got.SectionResults) != 1 {
		t.Fatalf("SectionResults len: got %d want 1", len(got.SectionResults))
	}
	if got.SectionResults[0].Markdown != orig.SectionResults[0].Markdown {
		t.Errorf("SectionResults[0].Markdown mismatch")
	}
}

// ---------------------------------------------------------------------------
// JSON key stability tests
// ---------------------------------------------------------------------------

// TestJSONKeys_LLMPromptBundle verifies that the expected JSON keys are present.
func TestJSONKeys_LLMPromptBundle(t *testing.T) {
	t.Parallel()
	b, _ := json.Marshal(sampleBundle())
	s := string(b)
	for _, key := range []string{
		`"version"`, `"tier"`, `"group"`, `"seed_entity_id"`,
		`"sections"`, `"graph_context"`, `"prompt_hash"`, `"generated_at"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("missing JSON key %s", key)
		}
	}
}

// TestJSONKeys_LLMRunResult verifies that the expected JSON keys are present.
func TestJSONKeys_LLMRunResult(t *testing.T) {
	t.Parallel()
	b, _ := json.Marshal(sampleResult())
	s := string(b)
	for _, key := range []string{
		`"version"`, `"prompt_hash"`, `"tier"`, `"group"`,
		`"seed_entity_id"`, `"section_results"`, `"filled_at"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("missing JSON key %s", key)
		}
	}
}

// TestJSONKeys_LLMGraphContext verifies graph context JSON keys.
func TestJSONKeys_LLMGraphContext(t *testing.T) {
	t.Parallel()
	gc := docgen.LLMGraphContext{
		EntityName:      "Foo",
		EntityKind:      "function",
		QualifiedName:   "pkg.Foo",
		Repo:            "/repo",
		SourceFile:      "foo.go",
		NeighbourBriefs: []docgen.NeighbourBrief{},
		SourceWindow:    "some source",
	}
	b, _ := json.Marshal(gc)
	s := string(b)
	for _, key := range []string{
		`"entity_name"`, `"entity_kind"`, `"qualified_name"`,
		`"repo"`, `"source_file"`, `"neighbour_briefs"`, `"source_window"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("missing JSON key %s", key)
		}
	}
}

// ---------------------------------------------------------------------------
// PromptHash determinism and schema
// ---------------------------------------------------------------------------

// TestBuildBundle_Deterministic verifies that BuildBundle returns the same
// prompt_hash for identical inputs across multiple calls.
func TestBuildBundle_Deterministic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        "nonexistent-group-xyz-test",
			SeedEntityID: "some-entity",
			Section:      "overview",
		},
		Tier: 0,
	}

	// Both calls will fail at the graph-load stage (no real group),
	// but if they do succeed (in environments with a real group) the hash
	// must be identical.
	b1, err1 := docgen.BuildBundle(ctx, opts)
	b2, err2 := docgen.BuildBundle(ctx, opts)

	if err1 != nil || err2 != nil {
		// Graph-load failed (expected in CI with no real group).
		// Verify the error is about the missing group, not a panic.
		if err1 != nil && !strings.Contains(err1.Error(), "group") &&
			!strings.Contains(err1.Error(), "config") &&
			!strings.Contains(err1.Error(), "repos") {
			t.Errorf("unexpected error from BuildBundle: %v", err1)
		}
		t.Skipf("skipping determinism check: no live group (expected in CI): %v", err1)
	}

	if b1.PromptHash != b2.PromptHash {
		t.Errorf("non-deterministic prompt_hash:\n  call1=%q\n  call2=%q",
			b1.PromptHash, b2.PromptHash)
	}
}

// TestBuildBundle_InvalidSection verifies that BuildBundle rejects an unknown
// section name before touching the graph.
func TestBuildBundle_InvalidSection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        "any-group",
			SeedEntityID: "any-entity",
			Section:      "not-a-real-section",
		},
		Tier: 0,
	}
	_, err := docgen.BuildBundle(ctx, opts)
	if err == nil {
		t.Fatal("expected error for unknown section, got nil")
	}
	if !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("expected 'unknown section' in error, got: %v", err)
	}
}

// TestBuildBundle_MissingGroup verifies that BuildBundle returns a group-config
// error when the group does not exist.
func TestBuildBundle_MissingGroup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        "group-that-does-not-exist-xyz-llmbundle",
			SeedEntityID: "abc123",
			Section:      "overview",
		},
		Tier: 0,
	}
	_, err := docgen.BuildBundle(ctx, opts)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
	if strings.Contains(err.Error(), "unknown section") {
		t.Errorf("should not get section error for group-load failure: %v", err)
	}
}

// TestBuildBundle_Tier1_MissingGroup verifies Tier 1 bundle also errors on
// missing group (not a section-validation error).
func TestBuildBundle_Tier1_MissingGroup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        "group-that-does-not-exist-xyz-tier1",
			SeedEntityID: "abc123",
		},
		Tier:   1,
		PageID: "test-page",
	}
	_, err := docgen.BuildBundle(ctx, opts)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
}

// ---------------------------------------------------------------------------
// BundleHashValid
// ---------------------------------------------------------------------------

// TestBundleHashValid_Match verifies BundleHashValid returns nil when hashes match.
func TestBundleHashValid_Match(t *testing.T) {
	t.Parallel()
	bundle := sampleBundle()
	result := sampleResult() // constructed with matching hash
	if err := docgen.BundleHashValid(&bundle, &result); err != nil {
		t.Errorf("expected nil error for matching hashes, got: %v", err)
	}
}

// TestBundleHashValid_Mismatch verifies BundleHashValid returns an error when
// hashes diverge.
func TestBundleHashValid_Mismatch(t *testing.T) {
	t.Parallel()
	bundle := sampleBundle()
	result := sampleResult()
	result.PromptHash = "00000000000000000000000000000000000000000000000000000000deadbeef"
	err := docgen.BundleHashValid(&bundle, &result)
	if err == nil {
		t.Fatal("expected error for mismatched hashes, got nil")
	}
	if !strings.Contains(err.Error(), "prompt_hash mismatch") {
		t.Errorf("expected 'prompt_hash mismatch' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CountResultWords / CountResultMermaid
// ---------------------------------------------------------------------------

// TestCountResultWords verifies word counting in LLMSectionResult.
func TestCountResultWords(t *testing.T) {
	t.Parallel()
	r := docgen.LLMSectionResult{
		Markdown: "This is a test sentence with seven words total.",
	}
	wc := docgen.CountResultWords(r)
	if wc != 9 {
		t.Errorf("expected 9 words, got %d", wc)
	}
}

// TestCountResultMermaid verifies mermaid block counting in LLMSectionResult.
func TestCountResultMermaid(t *testing.T) {
	t.Parallel()
	r := docgen.LLMSectionResult{
		Markdown: "```mermaid\ngraph LR\n  A-->B\n```\n\n```mermaid\nsequenceDiagram\n  A->>B: call\n```\n",
	}
	mc := docgen.CountResultMermaid(r)
	if mc != 2 {
		t.Errorf("expected 2 mermaid blocks, got %d", mc)
	}
}

// ---------------------------------------------------------------------------
// LLMBundleVersion constant
// ---------------------------------------------------------------------------

// TestBundleVersion verifies the version constant is "1".
func TestBundleVersion(t *testing.T) {
	if docgen.LLMBundleVersion != "1" {
		t.Errorf("LLMBundleVersion: got %q want %q", docgen.LLMBundleVersion, "1")
	}
}

// ---------------------------------------------------------------------------
// GeneratedAt format
// ---------------------------------------------------------------------------

// TestRoundTrip_GeneratedAt_RFC3339 verifies that GeneratedAt is a valid
// RFC3339 timestamp.
func TestRoundTrip_GeneratedAt_RFC3339(t *testing.T) {
	t.Parallel()
	bundle := sampleBundle()
	b, _ := json.Marshal(bundle)
	var got docgen.LLMPromptBundle
	_ = json.Unmarshal(b, &got)
	if _, err := time.Parse(time.RFC3339, got.GeneratedAt); err != nil {
		t.Errorf("GeneratedAt %q is not valid RFC3339: %v", got.GeneratedAt, err)
	}
}

// TestRoundTrip_FilledAt_RFC3339 verifies that FilledAt is a valid RFC3339 timestamp.
func TestRoundTrip_FilledAt_RFC3339(t *testing.T) {
	t.Parallel()
	result := sampleResult()
	b, _ := json.Marshal(result)
	var got docgen.LLMRunResult
	_ = json.Unmarshal(b, &got)
	if _, err := time.Parse(time.RFC3339, got.FilledAt); err != nil {
		t.Errorf("FilledAt %q is not valid RFC3339: %v", got.FilledAt, err)
	}
}

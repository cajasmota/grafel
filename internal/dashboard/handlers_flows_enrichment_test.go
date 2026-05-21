package dashboard

// handlers_flows_enrichment_test.go — unit tests for extractFlowDocs,
// docgenStatus, and enrichmentHealth (#1152).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// writeDocFile writes content to <dir>/<name> and returns the full path.
func writeDocFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeDocFile %s: %v", name, err)
	}
	return p
}

// fakeDocgenState builds a mcp.DocgenState whose GeneratedPaths are the given
// absolute file paths (used with identityResolver in tests).
func fakeDocgenState(paths []string) *mcp.DocgenState {
	return &mcp.DocgenState{GeneratedPaths: paths}
}

// identityResolver is a path resolver for tests that returns the path as-is.
// It is passed to extractFlowDocsWithResolver so tests can use absolute paths.
func identityResolver(p string) string { return p }

// ─────────────────────────────────────────────────────────────────────────────
// extractFlowDocs
// ─────────────────────────────────────────────────────────────────────────────

// TestExtractFlowDocs_fullFrontmatter — doc file with process_flow frontmatter
// containing all structured fields; entity ID appears in the path.
func TestExtractFlowDocs_fullFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	content := `---
entity_id: flow-checkout
kind: process_flow
rank: 0.95
group: checkout
group_label: 'Checkout flow'
summary: 'Processes a user checkout from cart to confirmation'
preconditions: 'User is authenticated and cart is non-empty'
expected_outcome: 'Order persisted, confirmation email sent'
steps:
  - Validate cart items
  - Charge payment method
  - Persist order record
  - Emit order.created event
gaps:
  - Missing error path for payment failure
---

## Description
Prose here.
`
	docPath := writeDocFile(t, tmp, "flow-checkout.md", content)
	state := fakeDocgenState([]string{docPath})

	fm, fallback := extractFlowDocsWithResolver("flow-checkout", state, identityResolver)
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fallback != "" {
		t.Errorf("fallback: expected empty, got %q", fallback)
	}
	assertStr(t, "kind", fm.Kind, "process_flow")
	assertStr(t, "summary", fm.Summary, "Processes a user checkout from cart to confirmation")
	assertStr(t, "preconditions", fm.Preconditions, "User is authenticated and cart is non-empty")
	assertStr(t, "expected_outcome", fm.ExpectedOutcome, "Order persisted, confirmation email sent")
	if len(fm.Steps) != 4 {
		t.Errorf("steps: expected 4, got %d: %v", len(fm.Steps), fm.Steps)
	}
	if len(fm.Gaps) != 1 {
		t.Errorf("gaps: expected 1, got %d", len(fm.Gaps))
	}
}

// TestExtractFlowDocs_noDocFile — no doc file present; should return (nil, "").
func TestExtractFlowDocs_noDocFile(t *testing.T) {
	state := fakeDocgenState([]string{"/nonexistent/path/flow-xyz.md"})
	fm, fallback := extractFlowDocsWithResolver("flow-xyz", state, identityResolver)
	if fm != nil {
		t.Fatalf("expected nil frontmatter for missing doc, got %+v", fm)
	}
	if fallback != "" {
		t.Errorf("fallback: expected empty for missing doc, got %q", fallback)
	}
}

// TestExtractFlowDocs_nilDocgenState — nil state returns (nil, "").
func TestExtractFlowDocs_nilDocgenState(t *testing.T) {
	fm, fallback := extractFlowDocsWithResolver("flow-anything", nil, identityResolver)
	if fm != nil {
		t.Fatalf("expected nil for nil docgenState")
	}
	if fallback != "" {
		t.Errorf("fallback: expected empty, got %q", fallback)
	}
}

// TestExtractFlowDocs_fallbackSummary — doc file exists but has no frontmatter;
// first-line fallback should be returned.
func TestExtractFlowDocs_fallbackSummary(t *testing.T) {
	tmp := t.TempDir()
	content := "# Flow title\n\nFirst prose line as fallback summary.\n"
	docPath := writeDocFile(t, tmp, "flow-abc.md", content)
	state := fakeDocgenState([]string{docPath})

	fm, fallback := extractFlowDocsWithResolver("flow-abc", state, identityResolver)
	if fm != nil {
		t.Fatalf("expected nil frontmatter for legacy doc, got %+v", fm)
	}
	if fallback == "" {
		t.Error("expected non-empty fallback for legacy doc")
	}
}

// TestExtractFlowDocs_tertiaryKindMatch — file path does not contain entity ID
// or "flow" but frontmatter has kind=process_flow and entity_id matches.
// Tertiary scan must find it.
func TestExtractFlowDocs_tertiaryKindMatch(t *testing.T) {
	tmp := t.TempDir()
	content := `---
entity_id: hashed-abc123
kind: process_flow
summary: 'Hashed-ID process flow'
preconditions: 'Authenticated'
expected_outcome: 'Event emitted'
---
`
	// File name gives no signal (neither entity ID nor "flow").
	docPath := writeDocFile(t, tmp, "generated-doc-XYZ.md", content)
	state := fakeDocgenState([]string{docPath})

	fm, _ := extractFlowDocsWithResolver("hashed-abc123", state, identityResolver)
	if fm == nil {
		t.Fatal("tertiary scan: expected non-nil frontmatter for kind-matched doc")
	}
	assertStr(t, "kind", fm.Kind, "process_flow")
	assertStr(t, "summary", fm.Summary, "Hashed-ID process flow")
}

// TestExtractFlowDocs_wrongKindIgnored — doc path contains "flow" but frontmatter
// has kind=http_endpoint; must not be returned as flow enrichment.
func TestExtractFlowDocs_wrongKindIgnored(t *testing.T) {
	tmp := t.TempDir()
	content := `---
entity_id: ep-flow-payments
kind: http_endpoint
summary: 'Payment endpoint'
---
`
	docPath := writeDocFile(t, tmp, "flow-payments.md", content)
	state := fakeDocgenState([]string{docPath})

	// Entity ID does not match; path contains "flow" but kind is http_endpoint.
	fm, fallback := extractFlowDocsWithResolver("unrelated-entity", state, identityResolver)
	if fm != nil {
		t.Fatalf("expected nil for non-matching kind, got %+v", fm)
	}
	if fallback != "" {
		t.Errorf("fallback: expected empty for wrong-kind doc, got %q", fallback)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// docgenStatus
// ─────────────────────────────────────────────────────────────────────────────

func TestDocgenStatus_enriched(t *testing.T) {
	fm := &EnrichmentFrontmatter{Kind: "process_flow", Summary: "A summary"}
	got := docgenStatus(fm, "")
	if got != "enriched" {
		t.Errorf("expected 'enriched', got %q", got)
	}
}

func TestDocgenStatus_pending(t *testing.T) {
	got := docgenStatus(nil, "")
	if got != "pending" {
		t.Errorf("expected 'pending', got %q", got)
	}
}

func TestDocgenStatus_stale(t *testing.T) {
	// Fallback is non-empty but no structured frontmatter.
	got := docgenStatus(nil, "First line from legacy doc")
	if got != "stale" {
		t.Errorf("expected 'stale', got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// enrichmentHealth
// ─────────────────────────────────────────────────────────────────────────────

func TestEnrichmentHealth_nil(t *testing.T) {
	h := enrichmentHealth(nil)
	for field, ok := range h {
		if ok {
			t.Errorf("nil fm: field %q should be false, got true", field)
		}
	}
}

func TestEnrichmentHealth_full(t *testing.T) {
	fm := &EnrichmentFrontmatter{
		Summary:         "s",
		Preconditions:   "p",
		ExpectedOutcome: "eo",
		Steps:           []string{"step1"},
		Gaps:            []string{"gap1"},
	}
	h := enrichmentHealth(fm)
	for field, ok := range h {
		if !ok {
			t.Errorf("full fm: field %q should be true, got false", field)
		}
	}
}

func TestEnrichmentHealth_partial(t *testing.T) {
	fm := &EnrichmentFrontmatter{Summary: "Only summary"}
	h := enrichmentHealth(fm)
	if !h["summary"] {
		t.Error("summary should be true")
	}
	if h["preconditions"] {
		t.Error("preconditions should be false")
	}
	if h["expected_outcome"] {
		t.Error("expected_outcome should be false")
	}
	if h["steps"] {
		t.Error("steps should be false")
	}
}

package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// parseFrontmatterBytes — unit tests
// ---------------------------------------------------------------------------

func TestParseFrontmatterBytes_absent(t *testing.T) {
	md := "# My doc\n\nSome prose here.\n"
	fm := parseFrontmatterBytes([]byte(md))
	if fm != nil {
		t.Fatalf("expected nil for doc without frontmatter, got %+v", fm)
	}
}

func TestParseFrontmatterBytes_malformed_no_close(t *testing.T) {
	md := "---\nentity_id: foo\nkind: http_endpoint\n# missing closing ---\n"
	fm := parseFrontmatterBytes([]byte(md))
	if fm != nil {
		t.Fatalf("expected nil for malformed frontmatter (no closing ---), got %+v", fm)
	}
}

func TestParseFrontmatterBytes_httpEndpoint(t *testing.T) {
	md := `---
entity_id: ep-123
kind: http_endpoint
disqualified: false
rank: 0.85
group: orders
group_label: 'Order processing'
summary: 'Returns paginated orders list'
gaps:
  - No error response for 4xx
  - Missing auth decorator
method: GET
path: /api/orders
auth: Bearer required
tables_touched: [orders, users]
---

## Description
Free-form prose.
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	// Universal fields.
	assertStr(t, "entity_id", fm.EntityID, "ep-123")
	assertStr(t, "kind", fm.Kind, "http_endpoint")
	if fm.Disqualified {
		t.Error("disqualified: expected false")
	}
	assertFloat(t, "rank", fm.Rank, 0.85)
	assertStr(t, "group", fm.Group, "orders")
	assertStr(t, "group_label", fm.GroupLabel, "Order processing")
	assertStr(t, "summary", fm.Summary, "Returns paginated orders list")
	if len(fm.Gaps) != 2 {
		t.Errorf("gaps: expected 2, got %d: %v", len(fm.Gaps), fm.Gaps)
	}
	// http_endpoint fields.
	assertStr(t, "method", fm.Method, "GET")
	assertStr(t, "path", fm.Path, "/api/orders")
	assertStr(t, "auth", fm.Auth, "Bearer required")
	if len(fm.TablesTouched) != 2 {
		t.Errorf("tables_touched: expected 2, got %d: %v", len(fm.TablesTouched), fm.TablesTouched)
	}
	assertStr(t, "tables_touched[0]", fm.TablesTouched[0], "orders")
	assertStr(t, "tables_touched[1]", fm.TablesTouched[1], "users")
}

func TestParseFrontmatterBytes_disqualified(t *testing.T) {
	md := `---
entity_id: noise-42
kind: http_endpoint
disqualified: true
summary: 'Regex noise endpoint'
---
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if !fm.Disqualified {
		t.Error("disqualified: expected true")
	}
}

func TestParseFrontmatterBytes_mergedInto(t *testing.T) {
	md := `---
entity_id: ep-old
kind: http_endpoint
merged_into: ep-new
summary: 'Deprecated duplicate'
---
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "merged_into", fm.MergedInto, "ep-new")
}

func TestParseFrontmatterBytes_processFlow(t *testing.T) {
	md := `---
entity_id: flow-order-checkout
kind: process_flow
rank: 0.95
group: checkout
group_label: 'Checkout flow'
summary: 'Processes a user checkout from cart to confirmation'
gaps:
  - Missing error path for payment failure
preconditions: 'User is authenticated and cart is non-empty'
expected_outcome: 'Order persisted, confirmation email sent'
steps:
  - Validate cart items
  - Charge payment method
  - Persist order record
  - Emit order.created event
---
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "kind", fm.Kind, "process_flow")
	assertStr(t, "preconditions", fm.Preconditions, "User is authenticated and cart is non-empty")
	assertStr(t, "expected_outcome", fm.ExpectedOutcome, "Order persisted, confirmation email sent")
	if len(fm.Steps) != 4 {
		t.Errorf("steps: expected 4, got %d: %v", len(fm.Steps), fm.Steps)
	}
	assertStr(t, "steps[0]", fm.Steps[0], "Validate cart items")
	if len(fm.Gaps) != 1 {
		t.Errorf("gaps: expected 1, got %d", len(fm.Gaps))
	}
}

func TestParseFrontmatterBytes_messageTopic(t *testing.T) {
	md := `---
entity_id: topic-order-created
kind: message_topic
rank: 0.9
group: order-events
group_label: 'Order events'
summary: 'Emitted when an order is successfully placed'
gaps:
  - No consumer registered in fulfillment service
schema: '{order_id, total, items}'
typical_payload_size_bytes: 512
volume_estimate: high
expected_consumers: [order-fulfillment, analytics, notifications]
---
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "kind", fm.Kind, "message_topic")
	assertStr(t, "schema", fm.Schema, "{order_id, total, items}")
	if fm.TypicalPayloadSizeBytes != 512 {
		t.Errorf("typical_payload_size_bytes: got %d want 512", fm.TypicalPayloadSizeBytes)
	}
	assertStr(t, "volume_estimate", fm.VolumeEstimate, "high")
	if len(fm.ExpectedConsumers) != 3 {
		t.Errorf("expected_consumers: expected 3, got %d: %v", len(fm.ExpectedConsumers), fm.ExpectedConsumers)
	}
}

func TestParseFrontmatterBytes_missingFields(t *testing.T) {
	// Only a subset of fields present — zeros expected for the rest.
	md := `---
kind: http_endpoint
summary: 'Minimal doc'
---
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "kind", fm.Kind, "http_endpoint")
	assertStr(t, "summary", fm.Summary, "Minimal doc")
	if fm.Rank != 0 {
		t.Errorf("rank: expected 0.0 (zero), got %f", fm.Rank)
	}
	if fm.Disqualified {
		t.Error("disqualified: expected false")
	}
	if fm.Group != "" {
		t.Errorf("group: expected empty, got %q", fm.Group)
	}
}

func TestParseFrontmatterBytes_multipleKindsBlock(t *testing.T) {
	// A doc with fields from multiple kinds should parse all of them without
	// error — the schema allows this (all fields optional, kind discriminates
	// at render time).
	md := `---
entity_id: multi
kind: http_endpoint
summary: 'Mixed kind fields'
method: POST
path: /api/checkout
preconditions: 'Cart non-empty'
---
`
	fm := parseFrontmatterBytes([]byte(md))
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "method", fm.Method, "POST")
	assertStr(t, "preconditions", fm.Preconditions, "Cart non-empty")
}

// ---------------------------------------------------------------------------
// ParseEnrichmentFrontmatter — file-based tests
// ---------------------------------------------------------------------------

func TestParseEnrichmentFrontmatter_absent(t *testing.T) {
	fm, err := ParseEnrichmentFrontmatter("/nonexistent/path/doc.md")
	if err != nil {
		t.Fatalf("unexpected error for absent file: %v", err)
	}
	if fm != nil {
		t.Fatalf("expected nil for absent file, got %+v", fm)
	}
}

func TestParseEnrichmentFrontmatter_file(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "ep.md")
	content := "---\nkind: http_endpoint\nsummary: 'From file'\n---\n\n# prose\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := ParseEnrichmentFrontmatter(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "kind", fm.Kind, "http_endpoint")
	assertStr(t, "summary", fm.Summary, "From file")
}

// ---------------------------------------------------------------------------
// extractEnrichmentFromFile — fallback behaviour tests
// ---------------------------------------------------------------------------

func TestExtractEnrichmentFromFile_frontmatterPreferred(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "ep.md")
	content := "---\nkind: http_endpoint\nsummary: 'Frontmatter summary'\n---\n\nFirst prose line.\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, fallback := extractEnrichmentFromFile(p)
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	assertStr(t, "summary", fm.Summary, "Frontmatter summary")
	if fallback != "" {
		t.Errorf("fallback: expected empty when frontmatter present, got %q", fallback)
	}
}

func TestExtractEnrichmentFromFile_fallback(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "ep.md")
	content := "# Heading\n\nFirst prose line.\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, fallback := extractEnrichmentFromFile(p)
	if fm != nil {
		t.Fatalf("expected nil frontmatter, got %+v", fm)
	}
	if fallback != "First prose line." {
		t.Errorf("fallback: got %q, want %q", fallback, "First prose line.")
	}
}

func TestExtractEnrichmentFromFile_absent(t *testing.T) {
	fm, fallback := extractEnrichmentFromFile("/nonexistent/ep.md")
	if fm != nil {
		t.Fatalf("expected nil for absent file")
	}
	if fallback != "" {
		t.Errorf("fallback: expected empty for absent file, got %q", fallback)
	}
}

// ---------------------------------------------------------------------------
// HasData
// ---------------------------------------------------------------------------

func TestHasData(t *testing.T) {
	var nilFM *EnrichmentFrontmatter
	if nilFM.HasData() {
		t.Error("nil.HasData() should be false")
	}
	empty := &EnrichmentFrontmatter{}
	if empty.HasData() {
		t.Error("empty.HasData() should be false")
	}
	withSummary := &EnrichmentFrontmatter{Summary: "x"}
	if !withSummary.HasData() {
		t.Error("summary-only.HasData() should be true")
	}
	withKind := &EnrichmentFrontmatter{Kind: "http_endpoint"}
	if !withKind.HasData() {
		t.Error("kind-only.HasData() should be true")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertStr(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("field %q: got %q, want %q", field, got, want)
	}
}

func assertFloat(t *testing.T, field string, got, want float64) {
	t.Helper()
	// Tolerate tiny float representation drift.
	diff := got - want
	if diff < -0.0001 || diff > 0.0001 {
		t.Errorf("field %q: got %f, want %f", field, got, want)
	}
}

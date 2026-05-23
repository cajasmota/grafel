package enrichment

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Schema / validation tests
// ---------------------------------------------------------------------------

func TestNewDocgenRepairCandidate_Valid(t *testing.T) {
	tests := []struct {
		name       string
		repairType string
		source     string
		target     string
		edgeKind   string
		newKind    string
		confidence float64
		evidence   string
	}{
		{
			name:       "resolve_ref",
			repairType: DocgenRepairResolveRef,
			source:     "entity-abc123",
			target:     "entity-def456",
			confidence: 0.9,
			evidence:   "auth.go@line 42: calls UserService directly",
		},
		{
			name:       "add_edge",
			repairType: DocgenRepairAddEdge,
			source:     "entity-abc123",
			target:     "entity-xyz999",
			edgeKind:   "CALLS",
			confidence: 0.85,
			evidence:   "handlers.go@line 77: dynamic dispatch via interface",
		},
		{
			name:       "fix_kind",
			repairType: DocgenRepairFixKind,
			source:     "entity-abc123",
			newKind:    "Service",
			confidence: 0.95,
			evidence:   "auth_service.go: file name and struct suffix confirm Service role",
		},
		{
			name:       "label_external",
			repairType: DocgenRepairLabelExternal,
			source:     "entity-abc123",
			target:     "ext:github.com/stripe/stripe-go",
			confidence: 1.0,
			evidence:   "payments.go@line 12: import stripe-go",
		},
		{
			name:       "merge_flow",
			repairType: DocgenRepairMergeFlow,
			source:     "flow-111",
			target:     "flow-222",
			confidence: 0.8,
			evidence:   "Both flows trace the same checkout path per Pass 4 analysis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewDocgenRepairCandidate(
				tt.repairType, tt.source, tt.target,
				tt.edgeKind, tt.newKind, tt.confidence,
				tt.evidence, "generate-docs/pass-3a",
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.ID == "" {
				t.Fatal("ID must not be empty")
			}
			if c.EmittedAt == "" {
				t.Fatal("EmittedAt must be set")
			}
		})
	}
}

func TestNewDocgenRepairCandidate_Invalid(t *testing.T) {
	tests := []struct {
		name       string
		repairType string
		source     string
		target     string
		newKind    string
		confidence float64
		evidence   string
		wantErr    string
	}{
		{
			name:       "unknown_type",
			repairType: "teleport_entity",
			source:     "e1",
			confidence: 0.9,
			evidence:   "test",
			wantErr:    "unknown type",
		},
		{
			name:       "missing_source",
			repairType: DocgenRepairResolveRef,
			source:     "",
			target:     "e2",
			confidence: 0.9,
			evidence:   "test",
			wantErr:    "source_entity_id is required",
		},
		{
			name:       "missing_evidence",
			repairType: DocgenRepairResolveRef,
			source:     "e1",
			target:     "e2",
			confidence: 0.9,
			evidence:   "",
			wantErr:    "evidence is required",
		},
		{
			name:       "confidence_above_1",
			repairType: DocgenRepairResolveRef,
			source:     "e1",
			target:     "e2",
			confidence: 1.5,
			evidence:   "test",
			wantErr:    "confidence must be in [0, 1]",
		},
		{
			name:       "confidence_negative",
			repairType: DocgenRepairResolveRef,
			source:     "e1",
			target:     "e2",
			confidence: -0.1,
			evidence:   "test",
			wantErr:    "confidence must be in [0, 1]",
		},
		{
			name:       "resolve_ref_missing_target",
			repairType: DocgenRepairResolveRef,
			source:     "e1",
			target:     "",
			confidence: 0.9,
			evidence:   "test",
			wantErr:    "target is required",
		},
		{
			name:       "fix_kind_missing_new_kind",
			repairType: DocgenRepairFixKind,
			source:     "e1",
			confidence: 0.9,
			evidence:   "test",
			wantErr:    "new_kind is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDocgenRepairCandidate(
				tt.repairType, tt.source, tt.target,
				"", tt.newKind, tt.confidence,
				tt.evidence, "test",
			)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantErr != "" && !containsStr(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDocgenRepairID_Stable(t *testing.T) {
	id1 := docgenRepairID(DocgenRepairResolveRef, "ent-111", "ent-222")
	id2 := docgenRepairID(DocgenRepairResolveRef, "ent-111", "ent-222")
	if id1 != id2 {
		t.Fatalf("IDs should be stable: %q vs %q", id1, id2)
	}
	id3 := docgenRepairID(DocgenRepairResolveRef, "ent-111", "ent-333")
	if id1 == id3 {
		t.Fatalf("IDs for different targets should differ")
	}
}

// ---------------------------------------------------------------------------
// Persistence tests
// ---------------------------------------------------------------------------

func TestAppendAndReadDocgenRepairs(t *testing.T) {
	dir := t.TempDir()

	c1, _ := NewDocgenRepairCandidate(DocgenRepairResolveRef, "ent-1", "ent-2", "", "", 0.9, "file.go@1", "pass-3a")
	c2, _ := NewDocgenRepairCandidate(DocgenRepairLabelExternal, "ent-3", "ext:stripe", "", "", 0.6, "payments.go@5", "pass-1a")

	if err := AppendDocgenRepair(dir, c1); err != nil {
		t.Fatalf("append c1: %v", err)
	}
	if err := AppendDocgenRepair(dir, c2); err != nil {
		t.Fatalf("append c2: %v", err)
	}

	candidates, skipped, err := ReadDocgenRepairs(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("unexpected skipped: %d", skipped)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].ID != c1.ID {
		t.Fatalf("first candidate ID mismatch: %q vs %q", candidates[0].ID, c1.ID)
	}
}

func TestReadDocgenRepairs_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cands, skipped, err := ReadDocgenRepairs(dir)
	if err != nil {
		t.Fatalf("unexpected error on missing file: %v", err)
	}
	if skipped != 0 || len(cands) != 0 {
		t.Fatalf("expected empty result")
	}
}

func TestReadDocgenRepairs_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docgen-repairs.jsonl")
	content := `{"type":"resolve_ref","source_entity_id":"e1","target":"e2","confidence":0.9,"evidence":"test","emitted_at":"2026-01-01T00:00:00Z"}
not-valid-json
{"type":"resolve_ref","source_entity_id":"e3","target":"e4","confidence":0.8,"evidence":"test2","emitted_at":"2026-01-01T00:00:00Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cands, skipped, err := ReadDocgenRepairs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skipped, got %d", skipped)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 valid candidates, got %d", len(cands))
	}
}

// ---------------------------------------------------------------------------
// Apply path tests
// ---------------------------------------------------------------------------

func TestApplyDocgenRepairs_HighConfApplied_LowConfQueued(t *testing.T) {
	dir := t.TempDir()

	// 1 high-confidence, 1 low-confidence, 1 edge-type (resolve_ref).
	high, _ := NewDocgenRepairCandidate(DocgenRepairResolveRef, "ent-1", "ent-2", "", "", 0.9, "auth.go@12", "pass-3a")
	low, _ := NewDocgenRepairCandidate(DocgenRepairAddEdge, "ent-3", "ent-4", "CALLS", "", 0.5, "handlers.go@7", "pass-3a")

	if err := AppendDocgenRepair(dir, high); err != nil {
		t.Fatal(err)
	}
	if err := AppendDocgenRepair(dir, low); err != nil {
		t.Fatal(err)
	}

	stats, err := ApplyDocgenRepairsToResolutions(dir, 100, 10)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if stats.Applied != 1 {
		t.Errorf("expected 1 applied, got %d", stats.Applied)
	}
	if stats.Queued != 1 {
		t.Errorf("expected 1 queued, got %d", stats.Queued)
	}

	// High-conf should appear in enrichment-resolutions.json.
	resPath := filepath.Join(dir, "enrichment-resolutions.json")
	data, err := os.ReadFile(resPath)
	if err != nil {
		t.Fatalf("read resolutions: %v", err)
	}
	var resos []map[string]any
	if err := json.Unmarshal(data, &resos); err != nil {
		t.Fatalf("parse resolutions: %v", err)
	}
	if len(resos) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(resos))
	}
	if resos[0]["id"] != high.ID {
		t.Errorf("resolution ID mismatch: %v vs %v", resos[0]["id"], high.ID)
	}

	// Low-conf should appear in docgen-repairs-pending.json.
	pending, err := ReadPendingDocgenRepairs(dir)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ID != low.ID {
		t.Errorf("pending ID mismatch: %v vs %v", pending[0].ID, low.ID)
	}
}

func TestApplyDocgenRepairs_FidelityDelta(t *testing.T) {
	dir := t.TempDir()

	// totalEdges=100, bugEdges=10 → fidelity_before = 0.90
	// Apply 1 resolve_ref (high confidence) → bug count drops by 1
	// fidelity_after = 0.91
	high, _ := NewDocgenRepairCandidate(DocgenRepairResolveRef, "ent-1", "ent-2", "", "", 0.9, "src.go@1", "test")
	if err := AppendDocgenRepair(dir, high); err != nil {
		t.Fatal(err)
	}

	stats, err := ApplyDocgenRepairsToResolutions(dir, 100, 10)
	if err != nil {
		t.Fatal(err)
	}

	if stats.FidelityBefore == nil || stats.FidelityAfter == nil {
		t.Fatal("expected fidelity values")
	}
	expectedBefore := 0.90
	expectedAfter := 0.91
	if math.Abs(*stats.FidelityBefore-expectedBefore) > 1e-9 {
		t.Errorf("fidelity_before: want %.4f got %.4f", expectedBefore, *stats.FidelityBefore)
	}
	if math.Abs(*stats.FidelityAfter-expectedAfter) > 1e-9 {
		t.Errorf("fidelity_after: want %.4f got %.4f", expectedAfter, *stats.FidelityAfter)
	}
}

func TestApplyDocgenRepairs_Idempotent(t *testing.T) {
	dir := t.TempDir()

	high, _ := NewDocgenRepairCandidate(DocgenRepairResolveRef, "ent-1", "ent-2", "", "", 0.9, "src.go@1", "test")
	if err := AppendDocgenRepair(dir, high); err != nil {
		t.Fatal(err)
	}

	// First apply.
	s1, err := ApplyDocgenRepairsToResolutions(dir, 50, 5)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Applied != 1 {
		t.Fatalf("first apply: expected 1 applied, got %d", s1.Applied)
	}

	// Second apply (same JSONL file) — should be a no-op because the ID is
	// already in enrichment-resolutions.json.
	s2, err := ApplyDocgenRepairsToResolutions(dir, 50, 5)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Applied != 0 {
		t.Errorf("second apply: expected 0 applied (idempotent), got %d", s2.Applied)
	}
	if s2.Skipped != 1 {
		t.Errorf("second apply: expected 1 skipped, got %d", s2.Skipped)
	}
}

func TestApplyDocgenRepairs_NoEdges_NoFidelity(t *testing.T) {
	dir := t.TempDir()
	// Pass (0, 0) — fidelity fields should be omitted.
	stats, err := ApplyDocgenRepairsToResolutions(dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FidelityBefore != nil || stats.FidelityAfter != nil {
		t.Error("fidelity should be nil when graph counts are unavailable")
	}
}

func TestPendingRepairs_MergesAcrossRuns(t *testing.T) {
	dir := t.TempDir()

	low1, _ := NewDocgenRepairCandidate(DocgenRepairAddEdge, "ent-1", "ent-2", "CALLS", "", 0.5, "a.go@1", "test")
	low2, _ := NewDocgenRepairCandidate(DocgenRepairAddEdge, "ent-3", "ent-4", "CALLS", "", 0.3, "b.go@1", "test")

	// First run — writes low1.
	if err := AppendDocgenRepair(dir, low1); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDocgenRepairsToResolutions(dir, 0, 0); err != nil {
		t.Fatal(err)
	}

	// Clear JSONL and write low2 only. Second run should preserve low1 in pending.
	if err := os.Remove(filepath.Join(dir, "docgen-repairs.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := AppendDocgenRepair(dir, low2); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDocgenRepairsToResolutions(dir, 0, 0); err != nil {
		t.Fatal(err)
	}

	pending, err := ReadPendingDocgenRepairs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending after two runs, got %d", len(pending))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsBytes(s, substr))
}

func containsBytes(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

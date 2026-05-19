package enrichment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// mkDoc builds a minimal Document for apply-path tests. Entities a/b/c
// are real (hex ids); rels start as bare stubs so they're candidates for
// repair.
func mkRepairDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "caller", SourceFile: "a.py", StartLine: 1, Language: "python"},
			{ID: "bbbbbbbbbbbbbbbb", Kind: "Function", Name: "target", SourceFile: "b.py", StartLine: 1, Language: "python"},
			{ID: "cccccccccccccccc", Kind: "Class", Name: "Wrapper", SourceFile: "a.py", StartLine: 1, Language: "python"},
		},
		Relationships: []graph.Relationship{
			// Bug-style stub edge.
			{ID: "r1", FromID: "aaaaaaaaaaaaaaaa", ToID: "Function:target", Kind: "CALLS"},
			// CONTAINS edge: Wrapper contains caller. Used in R4.
			{ID: "r2", FromID: "cccccccccccccccc", ToID: "aaaaaaaaaaaaaaaa", Kind: "CONTAINS"},
		},
	}
}

func edgeIDFor(t *testing.T, doc *graph.Document, idx int) string {
	t.Helper()
	r := doc.Relationships[idx]
	return repairEdgeID(r.FromID, r.Kind, r.ToID)
}

// TestApplyRepairs_BindToEntity_Success — happy path. Bind a bare stub to
// the real "bbbbbbbbbbbbbbbb" function. After apply, the ToID is rewritten
// and the edge carries resolved_by + repair_reasoning.
func TestApplyRepairs_BindToEntity_Success(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:         eid,
		Resolution:     RepairBindToEntity,
		TargetEntityID: "bbbbbbbbbbbbbbbb",
		Confidence:     0.9,
		Reasoning:      "imported from b.py per import line",
		Source:         "agent-repair",
		ResolvedAt:     "2026-05-19T07:00:00Z",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 1 {
		t.Fatalf("applied=%d, want 1; rejected=%+v", stats.AppliedCount, stats.Rejected)
	}
	r := doc.Relationships[0]
	if r.ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("ToID = %q, want hex target", r.ToID)
	}
	if r.Properties["resolved_by"] != "agent-repair" {
		t.Fatalf("resolved_by = %q, want agent-repair", r.Properties["resolved_by"])
	}
	if !strings.Contains(r.Properties["repair_reasoning"], "imported from b.py") {
		t.Fatalf("repair_reasoning = %q", r.Properties["repair_reasoning"])
	}
}

// R2 — bind_to_entity target must exist. Unknown id → rejected with
// target_entity_not_found; edge is left alone.
func TestApplyRepairs_R2_TargetEntityNotFound(t *testing.T) {
	doc := mkRepairDoc()
	original := doc.Relationships[0].ToID
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:         eid,
		Resolution:     RepairBindToEntity,
		TargetEntityID: "deadbeefdeadbeef",
		Reasoning:      "guessing",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 0 || len(stats.Rejected) != 1 {
		t.Fatalf("applied=%d rejected=%d, want 0/1", stats.AppliedCount, len(stats.Rejected))
	}
	if stats.Rejected[0].Reason != "target_entity_not_found" {
		t.Fatalf("reason = %q", stats.Rejected[0].Reason)
	}
	if doc.Relationships[0].ToID != original {
		t.Fatalf("edge mutated despite rejection: %q", doc.Relationships[0].ToID)
	}
}

// R3 — bind_to_entity must not be a self-loop.
func TestApplyRepairs_R3_SelfLoopDisallowed(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:         eid,
		Resolution:     RepairBindToEntity,
		TargetEntityID: "aaaaaaaaaaaaaaaa", // == FromID
		Reasoning:      "loop",
	}}, ApplyRepairsOptions{})
	if len(stats.Rejected) != 1 || stats.Rejected[0].Reason != "self_loop_disallowed" {
		t.Fatalf("rejected=%+v", stats.Rejected)
	}
}

// R4 — bind_to_entity cannot contradict CONTAINS hierarchy. doc has
// Wrapper CONTAINS caller; binding caller's CALLS edge to Wrapper would
// create a method→containing-class edge via repair.
func TestApplyRepairs_R4_ContradictsContainsHierarchy(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:         eid,
		Resolution:     RepairBindToEntity,
		TargetEntityID: "cccccccccccccccc", // CONTAINS-ancestor of FromID
		Reasoning:      "wrong",
	}}, ApplyRepairsOptions{})
	if len(stats.Rejected) != 1 || stats.Rejected[0].Reason != "contradicts_contains_hierarchy" {
		t.Fatalf("rejected=%+v", stats.Rejected)
	}
}

// R5 — repair becomes stale when no current edge matches its edge_id.
// Source moved → edge_id changed → repair is dropped (not applied),
// listed in stats.Stale.
func TestApplyRepairs_R5_StaleRepairDetected(t *testing.T) {
	doc := mkRepairDoc()
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:         "er:0000000000000000",
		Resolution:     RepairBindToEntity,
		TargetEntityID: "bbbbbbbbbbbbbbbb",
		Reasoning:      "x",
		ResolvedAt:     "2026-05-19T07:00:00Z",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 0 {
		t.Fatalf("stale repair was applied")
	}
	if len(stats.Stale) != 1 || stats.Stale[0].EdgeID != "er:0000000000000000" {
		t.Fatalf("stale = %+v", stats.Stale)
	}
}

// R6 — applied edges carry resolved_by=agent-repair tag.
func TestApplyRepairs_R6_ResolvedByTag(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	ApplyRepairs(doc, []Repair{{
		EdgeID:         eid,
		Resolution:     RepairBindToEntity,
		TargetEntityID: "bbbbbbbbbbbbbbbb",
		Reasoning:      "ok",
	}}, ApplyRepairsOptions{})
	if doc.Relationships[0].Properties["resolved_by"] != "agent-repair" {
		t.Fatalf("R6 violated")
	}
}

// R7 — reasoning text is persisted on the edge as repair_reasoning.
// (Trust-model R7 is also about empty reasoning being rejected.)
func TestApplyRepairs_R7_ReasoningPersistedAndRejectedIfEmpty(t *testing.T) {
	t.Run("persisted", func(t *testing.T) {
		doc := mkRepairDoc()
		eid := edgeIDFor(t, doc, 0)
		ApplyRepairs(doc, []Repair{{
			EdgeID:         eid,
			Resolution:     RepairBindToEntity,
			TargetEntityID: "bbbbbbbbbbbbbbbb",
			Reasoning:      "verbatim reasoning text",
		}}, ApplyRepairsOptions{})
		if doc.Relationships[0].Properties["repair_reasoning"] != "verbatim reasoning text" {
			t.Fatalf("reasoning lost: %q", doc.Relationships[0].Properties["repair_reasoning"])
		}
	})
	t.Run("rejected_if_empty", func(t *testing.T) {
		doc := mkRepairDoc()
		eid := edgeIDFor(t, doc, 0)
		stats := ApplyRepairs(doc, []Repair{{
			EdgeID:         eid,
			Resolution:     RepairBindToEntity,
			TargetEntityID: "bbbbbbbbbbbbbbbb",
			Reasoning:      "   ",
		}}, ApplyRepairsOptions{})
		if len(stats.Rejected) != 1 || stats.Rejected[0].Reason != "reasoning_too_short" {
			t.Fatalf("rejected=%+v", stats.Rejected)
		}
	})
}

// R-allowlist — unknown resolution kinds are rejected.
func TestApplyRepairs_ResolutionKindUnsupported(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:     eid,
		Resolution: "vibes_based",
		Reasoning:  "trust me",
	}}, ApplyRepairsOptions{})
	if len(stats.Rejected) != 1 || stats.Rejected[0].Reason != "resolution_kind_unsupported" {
		t.Fatalf("rejected=%+v", stats.Rejected)
	}
}

// reclassify_as_external happy path: ToID becomes ext:<module>.
func TestApplyRepairs_ReclassifyAsExternal_Success(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:     eid,
		Resolution: RepairReclassifyAsExternal,
		Module:     "django.db.models",
		Reasoning:  "imported from django",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 1 {
		t.Fatalf("applied=%d rejected=%+v", stats.AppliedCount, stats.Rejected)
	}
	if doc.Relationships[0].ToID != "ext:django.db.models" {
		t.Fatalf("ToID = %q", doc.Relationships[0].ToID)
	}
}

// R5-module-form — reclassify_as_external rejects path-traversal modules.
func TestApplyRepairs_InvalidModuleIdentifier(t *testing.T) {
	for _, bad := range []string{"../etc/passwd", "/absolute", "has spaces", ""} {
		t.Run(bad, func(t *testing.T) {
			doc := mkRepairDoc()
			eid := edgeIDFor(t, doc, 0)
			stats := ApplyRepairs(doc, []Repair{{
				EdgeID:     eid,
				Resolution: RepairReclassifyAsExternal,
				Module:     bad,
				Reasoning:  "x",
			}}, ApplyRepairsOptions{})
			if len(stats.Rejected) != 1 {
				t.Fatalf("rejected=%+v for module=%q", stats.Rejected, bad)
			}
			// Empty module is missing_required_field; others are invalid_module_identifier.
			want := "invalid_module_identifier"
			if bad == "" {
				want = "missing_required_field"
			}
			if stats.Rejected[0].Reason != want {
				t.Fatalf("reason=%q want=%q for module=%q", stats.Rejected[0].Reason, want, bad)
			}
		})
	}
}

// abandon: edge is dropped from doc.Relationships entirely.
func TestApplyRepairs_Abandon_DropsEdge(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	before := len(doc.Relationships)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:        eid,
		Resolution:    RepairAbandon,
		AbandonReason: "no useful binding exists",
		Reasoning:     "abandon",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 1 {
		t.Fatalf("applied=%d", stats.AppliedCount)
	}
	if len(doc.Relationships) != before-1 {
		t.Fatalf("rel count = %d, want %d", len(doc.Relationships), before-1)
	}
}

// reclassify_as_dynamic — ToID unchanged, dynamic_reason persisted.
func TestApplyRepairs_ReclassifyAsDynamic(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	originalToID := doc.Relationships[0].ToID
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:        eid,
		Resolution:    RepairReclassifyAsDynamic,
		DynamicReason: "env-var-route",
		Reasoning:     "name comes from $ROUTE",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 1 {
		t.Fatalf("applied=%d rejected=%+v", stats.AppliedCount, stats.Rejected)
	}
	if doc.Relationships[0].ToID != originalToID {
		t.Fatalf("dynamic reclassify changed ToID")
	}
	if doc.Relationships[0].Properties["dynamic_reason"] != "env-var-route" {
		t.Fatalf("dynamic_reason lost")
	}
}

// reclassify_as_resolved — ToID becomes new_target verbatim.
func TestApplyRepairs_ReclassifyAsResolved(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:     eid,
		Resolution: RepairReclassifyAsResolved,
		NewTarget:  "xrepo:other-svc::Handler.run",
		Reasoning:  "cross-repo target",
	}}, ApplyRepairsOptions{})
	if stats.AppliedCount != 1 {
		t.Fatalf("applied=%d rejected=%+v", stats.AppliedCount, stats.Rejected)
	}
	if doc.Relationships[0].ToID != "xrepo:other-svc::Handler.run" {
		t.Fatalf("ToID = %q", doc.Relationships[0].ToID)
	}
}

// missing_required_field — bind_to_entity without target_entity_id is rejected.
func TestApplyRepairs_MissingRequiredField(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{{
		EdgeID:     eid,
		Resolution: RepairBindToEntity,
		Reasoning:  "no target",
	}}, ApplyRepairsOptions{})
	if len(stats.Rejected) != 1 || stats.Rejected[0].Reason != "missing_required_field" {
		t.Fatalf("rejected=%+v", stats.Rejected)
	}
}

// duplicate edge_id — last wins, dup recorded as rejected.
func TestApplyRepairs_DuplicateEdgeID(t *testing.T) {
	doc := mkRepairDoc()
	eid := edgeIDFor(t, doc, 0)
	stats := ApplyRepairs(doc, []Repair{
		{EdgeID: eid, Resolution: RepairBindToEntity, TargetEntityID: "bbbbbbbbbbbbbbbb", Reasoning: "first"},
		{EdgeID: eid, Resolution: RepairBindToEntity, TargetEntityID: "bbbbbbbbbbbbbbbb", Reasoning: "second"},
	}, ApplyRepairsOptions{})
	if stats.AppliedCount != 1 {
		t.Fatalf("applied=%d", stats.AppliedCount)
	}
	gotDup := false
	for _, rj := range stats.Rejected {
		if rj.Reason == "duplicate_edge_id" {
			gotDup = true
		}
	}
	if !gotDup {
		t.Fatalf("expected duplicate_edge_id rejection, got %+v", stats.Rejected)
	}
}

// ReadRepairs round-trip from disk.
func TestReadRepairs_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	rf := repairFile{
		SchemaVersion: 1,
		Repairs: []Repair{{
			EdgeID: "er:abcdefabcdefabcd", Resolution: RepairBindToEntity,
			TargetEntityID: "1111111111111111", Confidence: 1,
			Reasoning: "x", Source: "agent-repair", ResolvedAt: "2026-05-19T00:00:00Z",
		}},
	}
	data, _ := json.MarshalIndent(rf, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "repair.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRepairs(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EdgeID != "er:abcdefabcdefabcd" {
		t.Fatalf("got = %+v", got)
	}
}

// ReadRepairs returns (nil, nil) when the file is absent.
func TestReadRepairs_Absent(t *testing.T) {
	tmp := t.TempDir()
	got, err := ReadRepairs(tmp)
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v, want nil/nil", got, err)
	}
}

// WriteRepairStats writes a valid JSON document with sorted slices.
func TestWriteRepairStats_Deterministic(t *testing.T) {
	tmp := t.TempDir()
	stats := RepairStats{
		SchemaVersion: 1,
		Applied:       []RepairAppliedRecord{{EdgeID: "er:b", Resolution: "bind_to_entity"}, {EdgeID: "er:a", Resolution: "abandon"}},
	}
	// Caller is expected to pre-sort; the writer doesn't re-sort.
	if err := WriteRepairStats(tmp, stats); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(tmp, "repair_stats.json"))
	if !strings.Contains(string(data), `"applied_count"`) {
		t.Fatalf("stats file = %s", data)
	}
}

// Integration check — when --enable-repair-apply is OFF (i.e. ApplyRepairs
// is never called), the document is byte-identical to baseline. We
// simulate this by checking that the function is a pure no-op when
// repairs is empty.
func TestApplyRepairs_NoRepairs_NoOp(t *testing.T) {
	doc := mkRepairDoc()
	beforeRels := len(doc.Relationships)
	stats := ApplyRepairs(doc, nil, ApplyRepairsOptions{})
	if stats.AppliedCount != 0 || stats.RejectedCount != 0 || stats.StaleCount != 0 {
		t.Fatalf("non-empty stats on empty repairs: %+v", stats)
	}
	if len(doc.Relationships) != beforeRels {
		t.Fatalf("relationships changed on empty repairs")
	}
}

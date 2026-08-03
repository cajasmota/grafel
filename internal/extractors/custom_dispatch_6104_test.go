package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6104 — MergeWithCustom destroyed and corrupted base entities.
//
// These tests pin the MERGE POLICY that replaces the old "custom wins by Name"
// rule. The policy has three tiers, keyed on the graph's OWN identity function
// (EntityRecord.ComputeID == sha256(OrgID+ProjectID+SourceFile+Kind+Name)):
//
//	Tier A  same (SourceFile, Kind, Name)      -> ONE node; the two records are
//	                                              COMBINED, never narrowed.
//	Tier B  Name collision, different identity -> TWO nodes; BOTH survive, the
//	                                              custom one ENRICHED from base.
//	Tier C  no collision                       -> appended unchanged.
//
// Two invariants hold across all three:
//
//	I1. A merge never loses an entity.
//	I2. A merge never narrows a span.

// ---- Tier B: cross-kind collision must not destroy the base entity ---------

// The python/Celery shape. `@shared_task def send_receipt` makes the base path
// emit BOTH `Task|send_receipt` and `SCOPE.Operation|send_receipt`, while the
// Celery custom extractor emits `SCOPE.Service|send_receipt`. Under Name-only
// keying the SCOPE.Service replaced one of them outright.
func TestMergeWithCustomCrossKindCollisionKeepsBothEntities(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "send_receipt", Kind: "Task", SourceFile: "app/tasks.py", StartLine: 4, EndLine: 6},
		{Name: "send_receipt", Kind: "SCOPE.Operation", Subtype: "function", SourceFile: "app/tasks.py", StartLine: 4, EndLine: 6},
	}
	custom := []types.EntityRecord{
		{Name: "send_receipt", Kind: "SCOPE.Service", Subtype: "task", SourceFile: "app/tasks.py", StartLine: 4, EndLine: 4},
	}

	got := MergeWithCustom(base, custom)

	want := map[string]bool{
		"Task|send_receipt":            false,
		"SCOPE.Operation|send_receipt": false,
		"SCOPE.Service|send_receipt":   false,
	}
	for _, e := range got {
		want[e.Kind+"|"+e.Name] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("entity %q was lost by the merge; got %v", k, kindNames(got))
		}
	}
	if len(got) != 3 {
		t.Errorf("expected 3 entities (nothing lost, nothing duplicated), got %d: %v", len(got), kindNames(got))
	}
}

// A Name collision across DIFFERENT source files is not a collision at all —
// the two records are different graph nodes and both must survive untouched.
func TestMergeWithCustomDifferentSourceFileKeepsBoth(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "list", Kind: "SCOPE.Operation", SourceFile: "a.java", StartLine: 1, EndLine: 9},
	}
	custom := []types.EntityRecord{
		{Name: "list", Kind: "SCOPE.Operation", SourceFile: "b.java", StartLine: 2, EndLine: 3},
	}
	got := MergeWithCustom(base, custom)
	if len(got) != 2 {
		t.Fatalf("expected both entities to survive, got %d: %v", len(got), kindNames(got))
	}
	for _, e := range got {
		if e.SourceFile == "a.java" && (e.StartLine != 1 || e.EndLine != 9) {
			t.Errorf("base entity in a.java was mutated by an unrelated file's custom entity: %d-%d", e.StartLine, e.EndLine)
		}
	}
}

// ---- Tier A: same identity is combined, and the span is never narrowed -----

// The java/Spring shape. Kind, Name and SourceFile are all identical, so these
// ARE the same graph node — one node out is correct. What was NOT correct was
// EndLine 12 -> 9: the body extent was silently truncated.
func TestMergeWithCustomSameIdentityNeverNarrowsSpan(t *testing.T) {
	base := []types.EntityRecord{{
		Name: "OrdersController.list", Kind: "SCOPE.Operation", Subtype: "method",
		QualifiedName: "api.OrdersController.list", SourceFile: "OrdersController.java",
		StartLine: 9, EndLine: 12,
	}}
	custom := []types.EntityRecord{{
		Name: "OrdersController.list", Kind: "SCOPE.Operation", Subtype: "endpoint",
		SourceFile: "OrdersController.java",
		StartLine:  9, EndLine: 9,
	}}

	got := MergeWithCustom(base, custom)
	if len(got) != 1 {
		t.Fatalf("same-identity records must collapse to one node, got %d: %v", len(got), kindNames(got))
	}
	s := got[0]
	if s.StartLine != 9 || s.EndLine != 12 {
		t.Errorf("span narrowed: want 9-12, got %d-%d", s.StartLine, s.EndLine)
	}
	if s.QualifiedName != "api.OrdersController.list" {
		t.Errorf("base QualifiedName lost, got %q", s.QualifiedName)
	}
}

// A custom extractor emitting a LATER start line must not clip the start of
// the span either — StartLine takes the minimum of the non-zero values.
func TestMergeWithCustomSameIdentityWidensStartLine(t *testing.T) {
	base := []types.EntityRecord{{Name: "h", Kind: "SCOPE.Operation", SourceFile: "s.go", StartLine: 4, EndLine: 20}}
	custom := []types.EntityRecord{{Name: "h", Kind: "SCOPE.Operation", SourceFile: "s.go", StartLine: 7, EndLine: 8}}
	got := MergeWithCustom(base, custom)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].StartLine != 4 || got[0].EndLine != 20 {
		t.Errorf("span narrowed: want 4-20, got %d-%d", got[0].StartLine, got[0].EndLine)
	}
}

// A base entity with no position (line 0) must not drag a positioned custom
// entity back to zero — 0 means "unknown", not "line zero". This is the JS
// route-operation shape, where the custom extractor is the one with positions.
func TestMergeWithCustomZeroBaseSpanDoesNotClobberCustomPosition(t *testing.T) {
	base := []types.EntityRecord{{Name: "GET /api/orders", Kind: "SCOPE.Operation", SourceFile: "server.js"}}
	custom := []types.EntityRecord{{Name: "GET /api/orders", Kind: "SCOPE.Operation", SourceFile: "server.js", StartLine: 5, EndLine: 5}}
	got := MergeWithCustom(base, custom)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].StartLine != 5 || got[0].EndLine != 5 {
		t.Errorf("want 5-5, got %d-%d", got[0].StartLine, got[0].EndLine)
	}
}

// The custom Subtype is genuinely more specific ("this method is an endpoint")
// and wins — but the base value it displaced is retained as provenance rather
// than discarded, so the information is enriched, not replaced.
func TestMergeWithCustomSameIdentityRetainsDisplacedSubtype(t *testing.T) {
	base := []types.EntityRecord{{
		Name: "OrdersController.list", Kind: "SCOPE.Operation", Subtype: "method",
		SourceFile: "OrdersController.java", StartLine: 9, EndLine: 12,
	}}
	custom := []types.EntityRecord{{
		Name: "OrdersController.list", Kind: "SCOPE.Operation", Subtype: "endpoint",
		SourceFile: "OrdersController.java", StartLine: 9, EndLine: 9,
	}}
	got := MergeWithCustom(base, custom)
	if got[0].Subtype != "endpoint" {
		t.Errorf("custom Subtype must win, got %q", got[0].Subtype)
	}
	if got[0].Properties[BaseSubtypeProperty] != "method" {
		t.Errorf("displaced base Subtype must be retained as %q provenance, got %q",
			BaseSubtypeProperty, got[0].Properties[BaseSubtypeProperty])
	}
}

// Two custom entities of the SAME identity must fold into the single node they
// name, not be appended alongside it as duplicate IDs.
func TestMergeWithCustomFoldsDuplicateCustomIdentities(t *testing.T) {
	base := []types.EntityRecord{{Name: "X", Kind: "SCOPE.Operation", SourceFile: "f.py", StartLine: 1, EndLine: 10}}
	custom := []types.EntityRecord{
		{Name: "X", Kind: "SCOPE.Operation", SourceFile: "f.py", Subtype: "endpoint", StartLine: 1, EndLine: 2},
		{Name: "X", Kind: "SCOPE.Operation", SourceFile: "f.py", Domain: "billing", StartLine: 1, EndLine: 3},
	}
	got := MergeWithCustom(base, custom)
	if len(got) != 1 {
		t.Fatalf("expected duplicate identities to fold into 1 node, got %d: %v", len(got), kindNames(got))
	}
	if got[0].EndLine != 10 {
		t.Errorf("span narrowed by fold: want EndLine 10, got %d", got[0].EndLine)
	}
	if got[0].Subtype != "endpoint" || got[0].Domain != "billing" {
		t.Errorf("fold lost custom state: subtype=%q domain=%q", got[0].Subtype, got[0].Domain)
	}
}

// ---- Tier B enrichment preserves what #4402 / #4366 / #4379 needed ---------

// #4402 carried the base QualifiedName and structural CONTAINS edges onto the
// survivor by DESTROYING the base node. Enrichment gets the same state onto
// the custom node while leaving the base node standing.
func TestMergeWithCustomCrossKindEnrichesCustomWithoutDestroyingBase(t *testing.T) {
	baseNode := types.EntityRecord{
		Name: "Contract", Kind: "SCOPE.Component", QualifiedName: "app.models.Contract",
		SourceFile: "m.py", StartLine: 3, EndLine: 30,
	}
	baseNode.ID = baseNode.ComputeID()
	baseNode.Relationships = []types.RelationshipRecord{
		{FromID: baseNode.ID, ToID: "Contract.amount", Kind: "CONTAINS"},
	}
	custom := []types.EntityRecord{
		{Name: "Contract", Kind: "SCOPE.Schema", Subtype: "model", SourceFile: "m.py"},
	}

	got := MergeWithCustom([]types.EntityRecord{baseNode}, custom)
	if len(got) != 2 {
		t.Fatalf("cross-kind collision must keep both nodes, got %d: %v", len(got), kindNames(got))
	}

	var b, c *types.EntityRecord
	for i := range got {
		switch got[i].Kind {
		case "SCOPE.Component":
			b = &got[i]
		case "SCOPE.Schema":
			c = &got[i]
		}
	}
	if b == nil || c == nil {
		t.Fatalf("expected both kinds present, got %v", kindNames(got))
	}
	// The base node survives INTACT — enrichment is one-directional.
	if b.QualifiedName != "app.models.Contract" || b.StartLine != 3 || b.EndLine != 30 {
		t.Errorf("base node was mutated: %+v", *b)
	}
	if len(b.Relationships) != 1 {
		t.Errorf("base node lost its structural edge: %v", b.Relationships)
	}
	// The custom node is enriched with base-only state (#4379, #4366).
	if c.QualifiedName != "app.models.Contract" {
		t.Errorf("custom node did not inherit base QualifiedName, got %q", c.QualifiedName)
	}
	if c.StartLine != 3 || c.EndLine != 30 {
		t.Errorf("custom node did not inherit the base span, got %d-%d", c.StartLine, c.EndLine)
	}
	customID := c.ID
	if customID == "" {
		customID = c.ComputeID()
	}
	var contains int
	for _, r := range c.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == "Contract.amount" {
			contains++
			if r.FromID != customID {
				t.Errorf("copied structural edge not re-keyed to the custom node: from=%s want=%s", r.FromID, customID)
			}
		}
	}
	if contains != 1 {
		t.Errorf("expected the base CONTAINS edge copied onto the custom node exactly once, got %d", contains)
	}
}

// ---- Re-keying edges produced by OTHER passes (#4402 gap) ------------------

// #4402 re-keyed only the superseded node's OWN self-edges. An edge emitted by
// a different pass, hanging off a different entity, was left pointing at an ID
// that no longer existed.
func TestMergeWithCustomRekeysForeignEdgesTargetingTheRetiredID(t *testing.T) {
	// The base record has not been ID-stamped yet, so its effective ID is the
	// computed one; the custom record arrives with an ID already stamped. The
	// survivor can only carry one of the two, so the other must be re-keyed.
	baseNode := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", StartLine: 1, EndLine: 9}
	retiredID := baseNode.ComputeID()

	// A third entity, emitted by an unrelated pass, calls the base node.
	caller := types.EntityRecord{Name: "caller", Kind: "SCOPE.Operation", SourceFile: "C.java"}
	caller.ID = caller.ComputeID()
	caller.Relationships = []types.RelationshipRecord{{FromID: caller.ID, ToID: retiredID, Kind: "CALLS"}}

	customNode := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", Subtype: "endpoint", StartLine: 1, EndLine: 1}
	customNode.ID = "custom-prestamped-id"

	got := MergeWithCustom([]types.EntityRecord{baseNode, caller}, []types.EntityRecord{customNode})
	baseNode.ID = retiredID // for the assertions below

	var survivorID string
	for _, e := range got {
		if e.Name == "list" {
			survivorID = e.ID
		}
	}
	if survivorID == "" {
		t.Fatalf("survivor not found: %v", kindNames(got))
	}
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.ToID == baseNode.ID && baseNode.ID != survivorID {
				t.Errorf("edge %s->%s (%s) still points at the retired base ID", r.FromID, r.ToID, r.Kind)
			}
			if r.Kind == "CALLS" && r.ToID != survivorID {
				t.Errorf("CALLS edge not re-keyed to survivor: to=%s want=%s", r.ToID, survivorID)
			}
		}
	}
}

// The merge must be able to see and re-key a STANDALONE relationship set, not
// only edges embedded on entity records — the signature #6104 asked for.
func TestMergeWithCustomRelsRekeysStandaloneRelationships(t *testing.T) {
	baseNode := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", StartLine: 1, EndLine: 9}
	retiredID := baseNode.ComputeID()
	customNode := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", Subtype: "endpoint", StartLine: 1, EndLine: 1}
	customNode.ID = "custom-prestamped-id"

	rels := []types.RelationshipRecord{
		{FromID: "someOtherPassNode", ToID: retiredID, Kind: "CALLS"},
		{FromID: retiredID, ToID: "someOtherPassNode", Kind: "USES"},
		{FromID: "x", ToID: "y", Kind: "IMPORTS"},
	}
	baseNode.ID = retiredID // for the assertions below

	ents, gotRels := MergeWithCustomRels([]types.EntityRecord{baseNode}, []types.EntityRecord{customNode}, rels)
	if len(ents) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(ents))
	}
	survivorID := ents[0].ID
	if len(gotRels) != 3 {
		t.Fatalf("relationship set must be preserved 1:1, got %d", len(gotRels))
	}
	for _, r := range gotRels {
		if r.FromID == baseNode.ID || r.ToID == baseNode.ID {
			if baseNode.ID != survivorID {
				t.Errorf("standalone edge %s->%s (%s) not re-keyed off the retired base ID", r.FromID, r.ToID, r.Kind)
			}
		}
	}
	if gotRels[2].FromID != "x" || gotRels[2].ToID != "y" {
		t.Errorf("unrelated edge was rewritten: %+v", gotRels[2])
	}
}

// kindNames renders a merged slice compactly for failure messages.
func kindNames(ents []types.EntityRecord) []string {
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.Kind+"|"+e.Name+"|"+e.SourceFile)
	}
	return out
}

// ---- ID preference (#6104 review finding M1) ------------------------------
//
// "The survivor prefers the base ID" is a policy claim, and it was unpinned:
// deleting the preference killed no test in any package. It is also
// CONDITIONALLY TRUE, which the pins below make explicit rather than papering
// over. Either way the `retired` map re-keys the losing ID, so nothing dangles
// — that is asserted here too, because it is the property that actually
// matters.

// When the base record HAS a stamped ID, the survivor carries it — that is the
// ID the rest of the base pass's edges already point at.
func TestMergeWithCustomPrefersTheBaseID(t *testing.T) {
	base := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", StartLine: 1, EndLine: 9}
	base.ID = "base-stamped-id"
	custom := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", Subtype: "endpoint", StartLine: 1, EndLine: 1}
	custom.ID = "custom-stamped-id"

	got := MergeWithCustom([]types.EntityRecord{base}, []types.EntityRecord{custom})
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	if got[0].ID != "base-stamped-id" {
		t.Errorf("survivor must carry the BASE id, got %q", got[0].ID)
	}
}

// When the base has NOT been stamped, the custom ID survives instead. ID
// stamping is per-extractor, so which side wins depends on which extractor
// happened to stamp — documented as conditional, and pinned so it cannot drift
// silently.
func TestMergeWithCustomKeepsCustomIDWhenBaseIsUnstamped(t *testing.T) {
	base := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", StartLine: 1, EndLine: 9}
	retired := base.ComputeID() // effective ID of the unstamped base
	custom := types.EntityRecord{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", Subtype: "endpoint", StartLine: 1, EndLine: 1}
	custom.ID = "custom-stamped-id"

	// A third entity points at the base's effective ID.
	caller := types.EntityRecord{Name: "caller", Kind: "SCOPE.Operation", SourceFile: "C.java", ID: "caller-id"}
	caller.Relationships = []types.RelationshipRecord{{FromID: "caller-id", ToID: retired, Kind: "CALLS"}}

	got := MergeWithCustom([]types.EntityRecord{base, caller}, []types.EntityRecord{custom})

	var survivorID string
	for _, e := range got {
		if e.Name == "list" {
			survivorID = e.ID
		}
	}
	if survivorID != "custom-stamped-id" {
		t.Errorf("with an unstamped base the CUSTOM id survives; got %q", survivorID)
	}
	// Whichever side loses, the retired ID must not be left dangling.
	for _, e := range got {
		for _, r := range e.Relationships {
			if r.FromID == retired || r.ToID == retired {
				t.Errorf("edge %s->%s (%s) still points at the retired base ID %s",
					r.FromID, r.ToID, r.Kind, retired)
			}
		}
	}
}

// ---- Disjoint spans (#6104 review finding M4) -----------------------------

// A blind span union invents a span covering code belonging to NEITHER entity.
// The Java method-overload shape reaches this: two declarations can share
// (Kind, Name, SourceFile) at different lines, so a custom record may combine
// against the wrong one. Disjoint spans are therefore NOT unioned.
func TestMergeWithCustomDoesNotUnionDisjointSpans(t *testing.T) {
	base := []types.EntityRecord{{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", StartLine: 5, EndLine: 8}}
	custom := []types.EntityRecord{{Name: "list", Kind: "SCOPE.Operation", SourceFile: "C.java", Subtype: "endpoint", StartLine: 40, EndLine: 60}}

	got := MergeWithCustom(base, custom)
	if len(got) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got))
	}
	s := got[0]
	if s.StartLine == 5 && s.EndLine == 60 {
		t.Fatalf("disjoint spans were unioned into 5-60, which covers code belonging to neither entity")
	}
	if s.StartLine != 5 || s.EndLine != 8 {
		t.Errorf("expected the BASE span 5-8 to be kept, got %d-%d", s.StartLine, s.EndLine)
	}
	if got := s.Properties[DisjointSpanProperty]; got != "40-60" {
		t.Errorf("the discarded custom span must be recorded as provenance, got %q", got)
	}
	// The refinement itself is still kept.
	if s.Subtype != "endpoint" {
		t.Errorf("subtype refinement lost, got %q", s.Subtype)
	}
}

// Touching and overlapping spans ARE unioned — the guard must not disable the
// never-narrow invariant for the ordinary case.
func TestMergeWithCustomUnionsOverlappingAndTouchingSpans(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		bStart, bEnd, cStart, cEnd int
		wantStart, wantEnd         int
	}{
		{"overlapping", 9, 12, 9, 9, 9, 12},
		{"touching", 5, 8, 8, 20, 5, 20},
		{"adjacent-contained", 5, 20, 7, 8, 5, 20},
		{"base position unknown", 0, 0, 5, 5, 5, 5},
		{"custom position unknown", 4, 20, 0, 0, 4, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := []types.EntityRecord{{Name: "h", Kind: "SCOPE.Operation", SourceFile: "s.go", StartLine: tc.bStart, EndLine: tc.bEnd}}
			custom := []types.EntityRecord{{Name: "h", Kind: "SCOPE.Operation", SourceFile: "s.go", StartLine: tc.cStart, EndLine: tc.cEnd}}
			got := MergeWithCustom(base, custom)
			if len(got) != 1 {
				t.Fatalf("expected 1 entity, got %d", len(got))
			}
			if got[0].StartLine != tc.wantStart || got[0].EndLine != tc.wantEnd {
				t.Errorf("want %d-%d, got %d-%d", tc.wantStart, tc.wantEnd, got[0].StartLine, got[0].EndLine)
			}
			if _, marked := got[0].Properties[DisjointSpanProperty]; marked {
				t.Errorf("non-disjoint spans must not be marked disjoint")
			}
		})
	}
}

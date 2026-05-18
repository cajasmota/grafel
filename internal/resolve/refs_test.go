package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func ent(id, kind, name string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: kind, Name: name, SourceFile: "x.go"}
}

func entAt(id, kind, name, file string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: kind, Name: name, SourceFile: file}
}

func TestReferences_Unambiguous(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("unambiguous: ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_Ambiguous(t *testing.T) {
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "Foo"),
		ent("bbbbbbbbbbbbbbbb", "Function", "Foo"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Foo", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Function:Foo" {
		t.Fatalf("ambiguous: ToID was rewritten to %s, expected stub preserved", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %d", stats.Ambiguous)
	}
}

func TestReferences_Unmatched(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Missing", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Function:Missing" {
		t.Fatalf("unmatched: ToID was rewritten: %s", rels[0].ToID)
	}
	if stats.Unmatched != 1 {
		t.Fatalf("expected 1 unmatched, got %d", stats.Unmatched)
	}
}

func TestReferences_KindAware(t *testing.T) {
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "User"),
		ent("bbbbbbbbbbbbbbbb", "View", "User"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:User", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("kind-aware: ToID resolved to wrong entity: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_StubMissingPrefix(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("bare-name fallback: ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_ScopePrefixedKind(t *testing.T) {
	// Pass 3 cross-language extractors emit kinds like "SCOPE.View". A stub
	// "View:User" must still resolve to that entity.
	entities := []types.EntityRecord{ent("cccccccccccccccc", "SCOPE.View", "Dashboard")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:Dashboard", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "cccccccccccccccc" {
		t.Fatalf("scope-prefixed kind: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_SkipsHexIDs(t *testing.T) {
	// Already-resolved IDs (16-char lower hex) must be left untouched.
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ffffffffffffffff", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "ffffffffffffffff" {
		t.Fatalf("hex-ID was modified: %s", rels[0].ToID)
	}
	if stats.Rewritten != 0 || stats.Ambiguous != 0 || stats.Unmatched != 0 {
		t.Fatalf("hex-ID counted in stats: %+v", stats)
	}
}

func TestReferencesEmbedded(t *testing.T) {
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{ToID: "Hello", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	if records[1].Relationships[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("embedded: ToID not rewritten: %s", records[1].Relationships[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

// PORT-2-FIX-3 (issue #31) — structural reference resolution + kind hint.

func TestReferences_StructuralFormatA(t *testing.T) {
	// scope:component:class:python:core/views/orders.py:OrderViewSet
	// resolves to the entity at that file+name.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "OrderViewSet", "core/views/orders.py"),
		entAt("dddddddddddddddd", "SCOPE.Component", "Other", "core/views/other.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "scope:component:class:python:core/views/orders.py:OrderViewSet",
		Kind:   "USES",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("format A: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_StructuralFormatB(t *testing.T) {
	// scope:operation:method:python:core/views/orders.py:OrderViewSet#create
	// resolves to the method entity recorded as "OrderViewSet.create".
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "OrderViewSet", "core/views/orders.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "OrderViewSet.create", "core/views/orders.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "scope:operation:method:python:core/views/orders.py:OrderViewSet#create",
		Kind:   "CALLS",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("format B: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_StructuralLocationAmbiguous(t *testing.T) {
	// Same (file, name) recorded twice → ambiguous, stub preserved.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "OrderViewSet", "core/views/orders.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.View", "OrderViewSet", "core/views/orders.py"),
	}
	stub := "scope:component:class:python:core/views/orders.py:OrderViewSet"
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: stub, Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != stub {
		t.Fatalf("location-ambig: ToID was rewritten to %s", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %+v", stats)
	}
}

func TestReferences_KindHintCallsPrefersOperation(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Operation", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("kind-hint CALLS: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_KindHintExtendsPrefersComponent(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Function", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "EXTENDS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("kind-hint EXTENDS: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_KindHintMissingStillAmbiguous(t *testing.T) {
	// Same setup as the disambiguation test, but the relationship's Kind
	// doesn't bias toward any family → still ambiguous, stub preserved.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Operation", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Foo" {
		t.Fatalf("hint-missing: ToID was rewritten to %s", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %+v", stats)
	}
}

// PORT-2-FIX-4 (issue #48) — FromID rewrite + kind-aware location index.

func TestReferencesEmbedded_FromIDRewritten(t *testing.T) {
	// Embedded relationship has a stub FromID. After ReferencesEmbedded,
	// FromID must be rewritten to the resolved entity ID.
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{FromID: "Function:Hello", ToID: "ffffffffffffffff", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	if got := records[1].Relationships[0].FromID; got != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("FromID not rewritten: %s", got)
	}
	if stats.FromRewritten != 1 {
		t.Fatalf("expected FromRewritten=1, got %+v", stats)
	}
}

func TestReferencesEmbedded_BothEndpointsRewritten(t *testing.T) {
	// Both FromID and ToID stubs; both should be rewritten and counted.
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{ID: "cccccccccccccccc", Kind: "Function", Name: "World", SourceFile: "c.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{FromID: "Function:Hello", ToID: "Function:World", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	rel := records[2].Relationships[0]
	if rel.FromID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("FromID not rewritten: %s", rel.FromID)
	}
	if rel.ToID != "cccccccccccccccc" {
		t.Fatalf("ToID not rewritten: %s", rel.ToID)
	}
	if stats.FromRewritten != 1 || stats.ToRewritten != 1 || stats.Rewritten != 2 {
		t.Fatalf("expected from=1 to=1 total=2, got %+v", stats)
	}
}

func TestReferences_SameFileKindDisambiguation(t *testing.T) {
	// Two entities at (a.py, "Foo") with different kinds. EXTENDS hint
	// biases toward Component family → resolves to Component entity.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "Foo", "a.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "Foo",
		Kind:   "EXTENDS",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("kind-disambig EXTENDS: ToID=%s", rels[0].ToID)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("expected ToRewritten=1, got %+v", stats)
	}
}

func TestReferences_SameFileKindNoHintStillAmbiguous(t *testing.T) {
	// Same setup as above, but the relationship Kind doesn't bias toward
	// any family → stub preserved (ambiguous), preserving existing
	// behavior when kind hint is absent.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "Foo", "a.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "Foo",
		Kind:   "USES",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Foo" {
		t.Fatalf("no-hint: ToID was rewritten to %s", rels[0].ToID)
	}
	if stats.ToAmbiguous != 1 {
		t.Fatalf("expected ToAmbiguous=1, got %+v", stats)
	}
}

func TestReferences_StatsTrackingPerEndpoint(t *testing.T) {
	// Mix of outcomes across both endpoints — verify the per-endpoint
	// counters tally correctly.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Function", "Hello", "a.go"),
	}
	rels := []types.RelationshipRecord{
		// from rewritten, to unmatched
		{FromID: "Function:Hello", ToID: "Function:Missing", Kind: "CALLS"},
		// from unmatched, to rewritten
		{FromID: "Function:Nope", ToID: "Function:Hello", Kind: "CALLS"},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if stats.FromRewritten != 1 {
		t.Fatalf("FromRewritten: %+v", stats)
	}
	if stats.FromUnmatched != 1 {
		t.Fatalf("FromUnmatched: %+v", stats)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("ToRewritten: %+v", stats)
	}
	if stats.ToUnmatched != 1 {
		t.Fatalf("ToUnmatched: %+v", stats)
	}
	if stats.Rewritten != 2 || stats.Unmatched != 2 {
		t.Fatalf("aggregates: %+v", stats)
	}
}

func TestReferences_StructuralFormatA_KindDisambiguation(t *testing.T) {
	// PORT-2-FIX-2 emits two entities at the same (file, name) with
	// different kinds (Component class + Operation method named the same
	// as the class). Format-A structural ref with scope-kind "component"
	// should pick the Component entity.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "CoreConfig", "core/apps.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "CoreConfig", "core/apps.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "scope:component:class:python:core/apps.py:CoreConfig",
		Kind:   "EXTENDS",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("structural-A kind-aware: ToID=%s", rels[0].ToID)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("expected ToRewritten=1, got %+v", stats)
	}
}

// VERIFY-2-PREP / issue #56 — disposition tagging.

// allowDjango is a tiny test-only ExternalAllowlist that recognises
// just "django" as a known external package. Keeps the tests free of an
// import on internal/external.
func allowDjango(pkg string) bool { return pkg == "django" }

func TestDisposition_Resolved(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionResolved]; got != 2 {
		t.Fatalf("expected 2 resolved (from + to), got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_ExternalKnown(t *testing.T) {
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ext:django", Kind: "CALLS"}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionExternalKnown]; got != 1 {
		t.Fatalf("expected 1 external-known, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_ExternalUnknown(t *testing.T) {
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ext:somerandompackage", Kind: "CALLS"}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionExternalUnknown]; got != 1 {
		t.Fatalf("expected 1 external-unknown, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_Dynamic(t *testing.T) {
	rels := []types.RelationshipRecord{{
		FromID:     "0000000000000000",
		ToID:       "getattr(self, name)",
		Kind:       "CALLS",
		Properties: map[string]string{"language": "python"},
	}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
		t.Fatalf("expected 1 dynamic, got %+v", stats.DispositionCounts)
	}
}

// TestDisposition_SourceFilePathFromIDIsDynamic (#120) — IMPORTS edges
// across every language extractor emit FromID = the importing file's
// source path. Without the file-path heuristic the FromID endpoint
// lands in bug-extractor for every IMPORTS edge in the graph; with it
// the endpoint is correctly tagged Dynamic (a structural identifier,
// not a missing entity).
func TestDisposition_SourceFilePathFromIDIsDynamic(t *testing.T) {
	rels := []types.RelationshipRecord{{
		FromID:     "src/main/java/com/foo/App.java",
		ToID:       "ext:org.springframework",
		Kind:       "IMPORTS",
		Properties: map[string]string{"language": "java"},
	}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, nil)
	// FromID (java path) → Dynamic; ToID (ext:...) → ExternalUnknown
	// because no allowlist supplied.
	if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
		t.Fatalf("expected 1 dynamic for source-file-path FromID, got %+v",
			stats.DispositionCounts)
	}
	if got := stats.DispositionCounts[DispositionBugExtractor]; got != 0 {
		t.Fatalf("expected 0 bug-extractor for source-file-path FromID, got %+v",
			stats.DispositionCounts)
	}
}

func TestDisposition_BugExtractor(t *testing.T) {
	// Graph has 0 entities named "NonexistentClass" → bug-extractor.
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "View", "RealView")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:NonexistentClass", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionBugExtractor]; got != 1 {
		t.Fatalf("expected 1 bug-extractor, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_BugResolver(t *testing.T) {
	// Three entities all named "Foo" under different kinds → bare-name
	// "Foo" with USES (no kind hint) is ambiguous, but the name DOES
	// exist in the graph → bug-resolver, not bug-extractor.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Function", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
		entAt("cccccccccccccccc", "View", "Foo", "c.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionBugResolver]; got != 1 {
		t.Fatalf("expected 1 bug-resolver, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_SampleCap(t *testing.T) {
	// 10 distinct unmatched stubs in the bug-extractor bucket — only 5
	// should be retained as samples.
	rels := make([]types.RelationshipRecord, 0, 10)
	for i := 0; i < 10; i++ {
		rels = append(rels, types.RelationshipRecord{
			FromID: "0000000000000000",
			ToID:   "View:Missing" + string(rune('A'+i)),
			Kind:   "USES",
		})
	}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionBugExtractor]; got != 10 {
		t.Fatalf("expected 10 bug-extractor counts, got %d", got)
	}
	if got := len(stats.DispositionSamples[DispositionBugExtractor]); got != 5 {
		t.Fatalf("expected 5 retained samples, got %d", got)
	}
}

func TestDisposition_BugRate(t *testing.T) {
	// 1 resolved (to) + 1 resolved (from hex) → 2 endpoints resolved.
	// 1 bug-extractor (View:Missing) + 1 bug-resolver (bare "Foo" with
	// 2 entities under different kinds + USES kind which doesn't bias).
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "Hello"),
		entAt("bbbbbbbbbbbbbbbb", "Function", "Foo", "a.py"),
		entAt("cccccccccccccccc", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"},
		{FromID: "0000000000000000", ToID: "View:Missing", Kind: "USES"},
		{FromID: "0000000000000000", ToID: "Foo", Kind: "USES"},
	}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)

	bugs := stats.DispositionCounts[DispositionBugExtractor] +
		stats.DispositionCounts[DispositionBugResolver]
	if bugs != 2 {
		t.Fatalf("expected 2 bug endpoints, got %+v", stats.DispositionCounts)
	}
	// 6 endpoints total (3 rels x 2 endpoints).
	expected := 2.0 / 6.0
	if stats.BugRate < expected-1e-9 || stats.BugRate > expected+1e-9 {
		t.Fatalf("bug rate: expected %.4f got %.4f counts=%+v", expected, stats.BugRate, stats.DispositionCounts)
	}
}

// TestDisposition_Unclassified covers the catch-all Disposition bucket
// (issue #81). DispositionUnclassified fires when an endpoint slips past
// every other check in classifyDispositionLang: it's not a hex ID, not an
// "ext:" placeholder, doesn't match a dynamic-dispatch pattern, and the
// name extracted from the stub is empty — leaving nothing for the bug-*
// classifier to look up. Real production stubs that hit this path include
// trailing-colon ("Kind:") forms emitted when an extractor failed to
// capture the target name, and malformed structural-refs whose tail
// segment is blank.
func TestDisposition_Unclassified(t *testing.T) {
	cases := []struct {
		name string
		stub string
	}{
		{
			// Bare "Kind:" with no name half — splitStub yields
			// ("Kind", "") → name == "" → Unclassified.
			name: "trailing colon empty name",
			stub: "View:",
		},
		{
			// Just the delimiter — both halves empty, kind-agnostic
			// path also has nothing to look up.
			name: "delimiter only",
			stub: ":",
		},
		{
			// Structural-ref with all 6 segments but a blank tail.
			// classifyDispositionLang's scope-aware extraction pulls
			// tail → name == "" → Unclassified. Note: the resolver's
			// lookupStructural separately rejects this with
			// statusUnmatched, but the disposition classifier still
			// has to bucket the endpoint.
			name: "scope ref with empty tail",
			stub: "scope:component:class:python:pkg/file.py:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rels := []types.RelationshipRecord{{
				FromID: "0000000000000000",
				ToID:   tc.stub,
				Kind:   "USES",
			}}
			idx := BuildIndex(nil)
			stats := ReferencesWithAllowlist(rels, idx, allowDjango)
			if got := stats.DispositionCounts[DispositionUnclassified]; got != 1 {
				t.Fatalf("stub %q: expected 1 unclassified, got counts=%+v",
					tc.stub, stats.DispositionCounts)
			}
			// Sanity: the catch-all must NOT leak into the bug-*
			// buckets — those fire only when a name was extracted.
			if got := stats.DispositionCounts[DispositionBugExtractor]; got != 0 {
				t.Fatalf("stub %q: unexpected bug-extractor count %d", tc.stub, got)
			}
			if got := stats.DispositionCounts[DispositionBugResolver]; got != 0 {
				t.Fatalf("stub %q: unexpected bug-resolver count %d", tc.stub, got)
			}
		})
	}
}

// TestDisposition_ReflectionBuiltinsBeatExternal is the regression test for
// issue #95. The external synthesiser used to add Python reflection
// builtins (getattr / setattr / hasattr / delattr / eval / exec / compile /
// __import__) and the JS Function constructor to its stdlib stop-list,
// which caused unresolved CALLS pointing at them to be rewritten to
// "ext:builtins" / "ext:globalThis" and then classified as
// ExternalUnknown — burying the dynamic-dispatch signal under
// external-import noise. After the fix, classifyDispositionLang runs the
// per-language dynamic-pattern catalog BEFORE the "ext:" prefix check, so
// the original reflection-builtin stub wins regardless of how synthesis
// rewrote the resolved ID.
func TestDisposition_ReflectionBuiltinsBeatExternal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		lang         string
		originalStub string
		// resolvedID simulates the post-synthesis state where the
		// synthesiser would (incorrectly) have stamped these builtins
		// with an "ext:<pkg>" prefix.
		resolvedID string
	}{
		{"py_getattr", "python", "getattr", "ext:builtins"},
		{"py_setattr", "python", "setattr", "ext:builtins"},
		{"py_hasattr", "python", "hasattr", "ext:builtins"},
		{"py_delattr", "python", "delattr", "ext:builtins"},
		{"py_eval", "python", "eval", "ext:builtins"},
		{"py_exec", "python", "exec", "ext:builtins"},
		{"py_compile", "python", "compile", "ext:builtins"},
		{"py_dunder_import", "python", "__import__", "ext:builtins"},
		{"py_getattr_call", "python", "getattr(self, name)", "ext:builtins"},
		{"py_eval_call", "python", "eval(src)", "ext:builtins"},
		{"js_eval", "javascript", "eval", "ext:globalThis"},
		{"js_function_ctor", "javascript", "Function", "ext:globalThis"},
		{"js_new_function", "javascript", "new Function(src)", "ext:globalThis"},
	}
	idx := BuildIndex(nil)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			endpoints := []EndpointPair{{
				FromID:     "0000000000000000",
				ToID:       tc.resolvedID,
				ToOriginal: tc.originalStub,
				Language:   tc.lang,
			}}
			stats := idx.ClassifyEndpoints(endpoints, allowDjango)
			if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
				t.Fatalf("%s: expected 1 dynamic, got counts=%+v",
					tc.originalStub, stats.DispositionCounts)
			}
			if got := stats.DispositionCounts[DispositionExternalUnknown]; got != 0 {
				t.Fatalf("%s: leaked into external-unknown (counts=%+v)",
					tc.originalStub, stats.DispositionCounts)
			}
		})
	}
}

// TestReflectionBuiltins_RecognisedAsDynamic asserts the resolver-side
// invariant for issue #95: every reflection builtin we care about is
// recognised as a dynamic pattern by isDynamicPatternLang for its
// language. The companion synthesiser-side guard
// (TestSynthesize_ReflectionBuiltinsLeftAlone in internal/external)
// covers the stdlibBareNames stop-list directly; this test does not
// inspect that map (it lives in another package and is unexported) —
// it only checks the per-language dynamic catalog (Refs #95).
func TestReflectionBuiltins_RecognisedAsDynamic(t *testing.T) {
	t.Parallel()
	// We import nothing from the external package here — that's a
	// separate test in internal/external. This test verifies the
	// resolver-side invariant that the per-language catalog catches
	// every reflection builtin on its own.
	reflectionBuiltins := []struct {
		stub string
		lang string
	}{
		{"getattr", "python"},
		{"setattr", "python"},
		{"hasattr", "python"},
		{"delattr", "python"},
		{"eval", "python"},
		{"exec", "python"},
		{"compile", "python"},
		{"__import__", "python"},
		{"eval", "javascript"},
		{"Function", "javascript"},
	}
	for _, b := range reflectionBuiltins {
		if !isDynamicPatternLang(b.stub, b.lang) {
			t.Fatalf("reflection builtin %q (%s) not recognised as dynamic", b.stub, b.lang)
		}
	}
}

// TestReferences_QualifiedNameMatch covers issue #100: a stub equal to an
// entity's QualifiedName (verbatim) must resolve directly. The markdown
// extractor emits Document --CONTAINS--> "<file>::<heading-slug>" edges
// where the ToID is exactly the heading entity's QualifiedName; before the
// fix splitStub split on the first ':' and the lookup landed in the
// bug-extractor bucket.
func TestReferences_QualifiedNameMatch(t *testing.T) {
	heading := types.EntityRecord{
		ID:            "abcdef0123456789",
		Kind:          "SCOPE.Heading",
		Name:          "Installation",
		QualifiedName: "README.md::installation",
		SourceFile:    "README.md",
	}
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: "README.md::installation", Kind: "CONTAINS"},
	}
	idx := BuildIndex([]types.EntityRecord{heading})
	stats := References(rels, idx)
	if rels[0].ToID != "abcdef0123456789" {
		t.Fatalf("QName resolution: ToID=%q, want abcdef0123456789", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

// TestReferences_QualifiedNameAmbiguous: two entities sharing a
// QualifiedName should leave the stub unresolved (ambiguous), never silently
// pick one.
func TestReferences_QualifiedNameAmbiguous(t *testing.T) {
	entities := []types.EntityRecord{
		{ID: "1111111111111111", Kind: "SCOPE.Heading", Name: "Foo",
			QualifiedName: "doc.md::foo", SourceFile: "doc.md"},
		{ID: "2222222222222222", Kind: "SCOPE.Heading", Name: "Foo",
			QualifiedName: "doc.md::foo", SourceFile: "doc.md"},
	}
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: "doc.md::foo", Kind: "CONTAINS"},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "doc.md::foo" {
		t.Fatalf("ambiguous QName should preserve stub, got %q", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %d", stats.Ambiguous)
	}
}

func TestBuildLocationIndex(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Component", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Bar", "a.py"),
		// Duplicate (file, name) → dropped.
		entAt("cccccccccccccccc", "Component", "Foo", "a.py"),
		entAt("dddddddddddddddd", "Component", "Foo", "b.py"),
	}
	loc := BuildLocationIndex(entities)
	if loc["a.py"]["Bar"] != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("a.py/Bar: %s", loc["a.py"]["Bar"])
	}
	if _, dup := loc["a.py"]["Foo"]; dup {
		t.Fatalf("a.py/Foo should be dropped as ambiguous; got %s", loc["a.py"]["Foo"])
	}
	if loc["b.py"]["Foo"] != "dddddddddddddddd" {
		t.Fatalf("b.py/Foo: %s", loc["b.py"]["Foo"])
	}
}

// TestRubyRailsDSL_RecognisedAsDynamic locks in issue #107: Rails
// ActionController DSL methods, ActiveRecord query builders, and
// ActiveRecord dynamic finders must be recognised as Dynamic by
// isDynamicPatternLang for lang="ruby". Pre-fix these landed in
// bug-extractor and drove rails-realworld bug-rate to 38.93% and
// sidekiq to 29.83%.
func TestRubyRailsDSL_RecognisedAsDynamic(t *testing.T) {
	t.Parallel()
	// Dynamic catalog (Rails DSL + ActiveRecord query builders +
	// dynamic finders). These all classify Dynamic for ruby source.
	dynamic := []string{
		// ActionController DSL
		"render", "permit", "require", "redirect_to", "respond_to",
		"before_action", "skip_before_action", "after_action",
		"around_action", "helper_method", "params", "session", "flash",
		"cookies", "request", "response",
		// Dynamic finders
		"find_by_email", "find_by_id", "find_by_name!",
		"find_or_create_by_slug", "find_or_create_by_email!",
		// ActiveRecord query DSL
		"order", "where", "joins", "includes", "eager_load", "preload",
		"pluck", "distinct", "group", "having", "limit", "offset",
		"scope", "belongs_to", "has_many", "has_one",
		"has_and_belongs_to_many", "validates", "validate",
		"before_save", "after_save", "before_create", "after_create",
		"before_destroy", "after_destroy",
		// ActiveRecord migration DSL (issue #124)
		"create_table", "drop_table", "change_table", "rename_table",
		"add_column", "remove_column", "rename_column", "change_column",
		"add_index", "remove_index", "add_reference", "remove_reference",
		"add_foreign_key", "remove_foreign_key",
		"references", "timestamps",
		"string", "integer", "boolean", "text", "datetime", "date",
		"float", "decimal", "binary", "execute",
	}
	for _, stub := range dynamic {
		stub := stub
		t.Run("ruby/"+stub, func(t *testing.T) {
			t.Parallel()
			if !isDynamicPatternLang(stub, "ruby") {
				t.Fatalf("Rails DSL stub %q not recognised as Dynamic for lang=ruby", stub)
			}
		})
	}
}

// TestRubyRailsDSL_GatedToRuby confirms the Rails DSL bare-name patterns
// only match when lang="ruby" — otherwise generic names like `where`,
// `order`, `params`, `request` would shadow user methods in JS / Go /
// Python codebases (lesson from #94 / #105).
func TestRubyRailsDSL_GatedToRuby(t *testing.T) {
	t.Parallel()
	stubs := []string{
		"where", "order", "params", "request", "response", "limit",
		"render",
		// NOTE: `group` was removed from this gate list (issue #423) —
		// click's `@click.group()` decorator legitimately makes `group`
		// a Python DSL bare-name too, so it is intentionally NOT
		// ruby-only any more. Per-language scoping still holds for the
		// remaining names.
		// NOTE: `validates` was removed from this gate list (issue
		// #446) — Marshmallow's `@validates(...)` decorator
		// legitimately makes `validates` a Python DSL bare-name too,
		// so it is intentionally NOT ruby-only any more.
		// Migration DSL gating (issue #124) — collision-prone for
		// other languages (`string`, `integer`, `boolean`, `text`,
		// `references`, `execute`, `add_column`...).
		"string", "integer", "boolean", "text", "references",
		"execute", "create_table", "add_index", "timestamps",
	}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", "kotlin"}
	for _, stub := range stubs {
		for _, lang := range otherLangs {
			stub, lang := stub, lang
			t.Run(stub+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if isDynamicPatternLang(stub, lang) {
					t.Fatalf("stub %q matched dynamic for lang=%q; "+
						"must be gated to ruby only", stub, lang)
				}
			})
		}
	}
}

// TestRubyRailsDSL_RejectedGenericCollectionOps locks in the explicit
// rejection list from issue #107: generic collection ops (each / map /
// select / find / count / length / size) MUST NOT be classified
// Dynamic — they're plain Enumerable methods, used everywhere, and
// hiding them would mask real missing-resolution bugs.
func TestRubyRailsDSL_RejectedGenericCollectionOps(t *testing.T) {
	t.Parallel()
	rejected := []string{"each", "map", "select", "find", "count", "length", "size"}
	for _, stub := range rejected {
		stub := stub
		t.Run("ruby/"+stub, func(t *testing.T) {
			t.Parallel()
			if isDynamicPatternLang(stub, "ruby") {
				t.Fatalf("generic collection op %q matched dynamic for ruby; "+
					"must NOT be in catalog (issue #107 rejection list)", stub)
			}
		})
	}
}

// TestPythonFlaskDSL_RecognisedAsDynamic locks in issue #420: Flask
// app-factory + decorator DSL bindings (`@app.route(...)`,
// `@bp.route(...)`, `@bp.cli.command(...)`, lifecycle hooks, error
// handlers, template filters, URL preprocessors). Pre-fix these
// landed in bug-extractor and drove flask + flask-realworld bug-rate
// to 43.93% / 43.63%. The Python extractor strips the receiver and
// emits only the leaf callee identifier, so the resolver needs a
// per-language bare-name anchor to classify them as Dynamic.
func TestPythonFlaskDSL_RecognisedAsDynamic(t *testing.T) {
	t.Parallel()
	dynamic := []string{
		// Routing
		"route", "add_url_rule", "register_blueprint",
		// Lifecycle
		"before_request", "before_first_request", "after_request",
		"teardown_request", "teardown_appcontext",
		// Error handling
		"errorhandler", "register_error_handler",
		// Context / templates
		"shell_context_processor", "context_processor",
		"template_filter", "template_test", "template_global",
		"url_value_preprocessor", "url_defaults",
		// Blueprint app-scoped variants
		"before_app_request", "before_app_first_request",
		"after_app_request", "teardown_app_request",
		"app_errorhandler", "app_context_processor",
		"app_template_filter", "app_template_test", "app_template_global",
		"app_url_value_preprocessor", "app_url_defaults",
		"record", "record_once",
		// Flask CLI / click AppGroup decorator
		"command",
	}
	for _, stub := range dynamic {
		stub := stub
		t.Run("python/"+stub, func(t *testing.T) {
			t.Parallel()
			if !isDynamicPatternLang(stub, "python") {
				t.Fatalf("Flask DSL stub %q not recognised as Dynamic for lang=python", stub)
			}
		})
	}
}

// TestPythonFlaskDSL_GatedToPython confirms the Flask DSL bare-name
// patterns only match when lang="python" — otherwise generic names
// like `route`, `command`, `record`, `before_request` would shadow
// user methods in other ecosystems.
func TestPythonFlaskDSL_GatedToPython(t *testing.T) {
	t.Parallel()
	stubs := []string{
		"route", "command", "record", "errorhandler",
		"before_request", "after_request", "register_blueprint",
		"template_filter", "url_defaults",
	}
	otherLangs := []string{"go", "ruby", "javascript", "typescript", "java", "kotlin"}
	for _, stub := range stubs {
		for _, lang := range otherLangs {
			stub, lang := stub, lang
			t.Run(stub+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if isDynamicPatternLang(stub, lang) {
					t.Fatalf("stub %q matched dynamic for lang=%q; "+
						"must be gated to python only (issue #420)", stub, lang)
				}
			})
		}
	}
}

// TestPythonFlaskExtensionsDSL_RecognisedAsDynamic locks in issue #446:
// Flask extension + Marshmallow + Flask-SQLAlchemy DSL bindings. Pre-fix
// the residual on python/flask (41.32%) and python/flask-realworld
// (43.47%) was dominated by these bare-name leaves emitted after the
// Python extractor strips receivers like `db.`, `current_user.`,
// `fields.`, `Schema.`, `form.`, `app.`. Mirrors the Flask (#420) and
// click (#423) DSL precedent. Per-language gate (Python only) keeps
// generic names like `add`, `delete`, `commit`, `dump`, `load` from
// shadowing user methods in other ecosystems — within Python the
// trade is accepted (same precedent as Rails `render`/`session` etc.
// in #107) because Dynamic is the appropriate bucket for framework
// dispatch we can't statically resolve.
func TestPythonFlaskExtensionsDSL_RecognisedAsDynamic(t *testing.T) {
	t.Parallel()
	dynamic := []string{
		// Flask-SQLAlchemy: column / type / relationship constructors and
		// session/query methods on `db` (SQLAlchemy()) instances.
		"Column", "ForeignKey", "relationship", "backref",
		"Integer", "String", "Text", "Boolean", "DateTime",
		"Date", "Float", "Numeric",
		"init_app", "query", "query_property", "create_all", "drop_all",
		"session", "commit", "rollback", "flush",
		"add", "delete", "merge", "refresh",
		// Flask-Login
		"current_user", "login_required", "login_user", "logout_user", "confirm_login",
		// Flask-WTF
		"validate_on_submit", "populate_obj", "render_kw",
		// Marshmallow — schema field constructors and (de)serialization hooks.
		// `Boolean`/`DateTime` are deliberately not re-listed (already above).
		"fields", "Schema", "Str", "Int", "List", "Nested", "Method", "Function",
		"pre_load", "post_load", "pre_dump", "post_dump",
		"validates", "validates_schema",
		"dump", "load", "dumps", "loads",
		// Flask common response helpers
		"jsonify", "make_response", "abort", "send_file",
		"send_from_directory", "stream_with_context",
	}
	for _, stub := range dynamic {
		stub := stub
		t.Run("python/"+stub, func(t *testing.T) {
			t.Parallel()
			if !isDynamicPatternLang(stub, "python") {
				t.Fatalf("Flask extension DSL stub %q not recognised as Dynamic for lang=python", stub)
			}
		})
	}
}

// TestPythonFlaskExtensionsDSL_GatedToPython confirms the Flask
// extension / Marshmallow / Flask-SQLAlchemy bare-name patterns only
// match when lang="python" — otherwise generic names like `add`,
// `delete`, `commit`, `session`, `query`, `dump`, `load`, `fields`,
// `String`, `Integer` would shadow user methods/types in other
// ecosystems trivially.
func TestPythonFlaskExtensionsDSL_GatedToPython(t *testing.T) {
	t.Parallel()
	// NOTE: `session` is intentionally NOT in this list — the Ruby
	// DSL block (#107) also claims `session` for Rails, so the
	// per-language assertion against ruby would fail. The Python
	// gate still applies to `session` for non-ruby languages, but
	// asserting only the non-overlap cases keeps the test honest.
	// `delete` is also excluded — the Rails ActionPack additions
	// (#448) claim it as a routing-DSL verb in rubyDynamicPatterns,
	// so the dynamic-pattern check fires for ruby and the negative
	// assertion would trip.
	stubs := []string{
		"add", "commit", "query",
		"dump", "load", "fields", "Schema", "Column",
		"String", "Integer", "current_user", "login_required",
		"jsonify", "abort",
	}
	otherLangs := []string{"go", "ruby", "javascript", "typescript", "java", "kotlin"}
	for _, stub := range stubs {
		for _, lang := range otherLangs {
			stub, lang := stub, lang
			t.Run(stub+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if isDynamicPatternLang(stub, lang) {
					t.Fatalf("stub %q matched dynamic for lang=%q; "+
						"must be gated to python only (issue #446)", stub, lang)
				}
			})
		}
	}
}

// TestDiagnoseBugResolver — issue #92 diagnostic helper. Each subtest
// constructs a graph that traps a bug-resolver stub at one well-defined
// shape and asserts the category the diagnoser returns.
func TestDiagnoseBugResolver(t *testing.T) {
	t.Run("ambig-bare-no-hint", func(t *testing.T) {
		// Two Functions named "create" → bare "create" call with relKind=CONTAINS
		// (no hint family registered) is ambig-bare-no-hint.
		entities := []types.EntityRecord{
			ent("aaaaaaaaaaaaaaaa", "Function", "create"),
			ent("bbbbbbbbbbbbbbbb", "Function", "create"),
		}
		idx := BuildIndex(entities)
		got := idx.DiagnoseBugResolver("create", "CONTAINS")
		if got.Category != "ambig-bare-no-hint" {
			t.Fatalf("category: got %q, want ambig-bare-no-hint (diag=%+v)", got.Category, got)
		}
		if got.Name != "create" {
			t.Fatalf("name: got %q want create", got.Name)
		}
	})

	t.Run("ambig-bare-hint-fail", func(t *testing.T) {
		// Two Operations named "validate" → bare call with relKind=CALLS:
		// hint family is operation, but multiple Operation matches → fail.
		entities := []types.EntityRecord{
			ent("aaaaaaaaaaaaaaaa", "Operation", "validate"),
			ent("bbbbbbbbbbbbbbbb", "Operation", "validate"),
		}
		idx := BuildIndex(entities)
		got := idx.DiagnoseBugResolver("validate", "CALLS")
		if got.Category != "ambig-bare-hint-fail" {
			t.Fatalf("category: got %q, want ambig-bare-hint-fail (diag=%+v)", got.Category, got)
		}
		if len(got.HintFamily) == 0 {
			t.Fatalf("HintFamily empty for CALLS")
		}
	})

	t.Run("kind-mismatch", func(t *testing.T) {
		// Stub is "Operation:foo" but graph has Schema:foo only. Kind
		// bucket misses, kind-bucket has no entity, name lives under a
		// different kind family entirely.
		entities := []types.EntityRecord{
			ent("aaaaaaaaaaaaaaaa", "Schema", "foo"),
		}
		idx := BuildIndex(entities)
		got := idx.DiagnoseBugResolver("Operation:foo", "CALLS")
		if got.Category != "kind-mismatch" {
			t.Fatalf("category: got %q, want kind-mismatch (diag=%+v)", got.Category, got)
		}
		if got.StubKind != "Operation" {
			t.Fatalf("StubKind: got %q want Operation", got.StubKind)
		}
	})

	t.Run("ambig-kind", func(t *testing.T) {
		// Two Functions named "Bar" → "Function:Bar" stub is ambiguous
		// within its kind.
		entities := []types.EntityRecord{
			ent("aaaaaaaaaaaaaaaa", "Function", "Bar"),
			ent("bbbbbbbbbbbbbbbb", "Function", "Bar"),
		}
		idx := BuildIndex(entities)
		got := idx.DiagnoseBugResolver("Function:Bar", "CALLS")
		if got.Category != "ambig-kind" {
			t.Fatalf("category: got %q, want ambig-kind (diag=%+v)", got.Category, got)
		}
	})

	t.Run("empty-stub", func(t *testing.T) {
		idx := BuildIndex(nil)
		got := idx.DiagnoseBugResolver("", "CALLS")
		if got.Category != "unknown" {
			t.Fatalf("category for empty stub: got %q want unknown", got.Category)
		}
	})

	t.Run("ambig-qualified", func(t *testing.T) {
		// Two entities sharing the same QualifiedName cause BuildIndex to
		// blank the byQualifiedName entry. A stub equal to that QualifiedName
		// is then diagnosed as ambig-qualified — the diagnoser short-circuits
		// before kind/name splitting.
		entities := []types.EntityRecord{
			{ID: "1111111111111111", Kind: "SCOPE.Heading", Name: "Foo",
				QualifiedName: "doc.md::foo", SourceFile: "doc.md"},
			{ID: "2222222222222222", Kind: "SCOPE.Heading", Name: "Foo",
				QualifiedName: "doc.md::foo", SourceFile: "doc.md"},
		}
		idx := BuildIndex(entities)
		got := idx.DiagnoseBugResolver("doc.md::foo", "CONTAINS")
		if got.Category != "ambig-qualified" {
			t.Fatalf("category: got %q, want ambig-qualified (diag=%+v)", got.Category, got)
		}
		if got.Name != "doc.md::foo" {
			t.Fatalf("name: got %q want doc.md::foo", got.Name)
		}
	})

	t.Run("kindsPresent-fallback", func(t *testing.T) {
		// Bare-name stub for a name that appears in nameKinds but is NOT in
		// ambigName and has no Kind: prefix. A single entity registers its
		// name under nameKinds while leaving ambigName false, so the
		// diagnoser falls through the kind-prefix and ambig-bare branches
		// and lands on the kindsPresent-fallback case (returns ambig-kind
		// per the histogram bucketing comment in DiagnoseBugResolver).
		entities := []types.EntityRecord{
			ent("aaaaaaaaaaaaaaaa", "Function", "loneFn"),
		}
		idx := BuildIndex(entities)
		got := idx.DiagnoseBugResolver("loneFn", "")
		if got.Category != "ambig-kind" {
			t.Fatalf("category: got %q, want ambig-kind via kindsPresent fallback (diag=%+v)", got.Category, got)
		}
		if len(got.KindsPresent) == 0 {
			t.Fatalf("KindsPresent should be populated from nameKinds bucket; diag=%+v", got)
		}
		if got.StubKind != "" {
			t.Fatalf("StubKind should be empty for bare-name stub; got %q", got.StubKind)
		}
	})
}

// Issue #140 — Ruby class CONTAINS edges must use a structural-ref so that
// same-named methods (e.g. Rails `create` in many controllers) resolve to
// the file-local method rather than landing in the bug-resolver bucket.
//
// Two Ruby files each define a `create` method inside a class. The CONTAINS
// edges from each class must rewrite to the matching file-local method ID,
// not to the other file's `create`.
func TestReferences_RubyClassContainsStructuralRef(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "UsersController", "app/controllers/users_controller.rb"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "create", "app/controllers/users_controller.rb"),
		entAt("cccccccccccccccc", "SCOPE.Component", "PostsController", "app/controllers/posts_controller.rb"),
		entAt("dddddddddddddddd", "SCOPE.Operation", "create", "app/controllers/posts_controller.rb"),
	}
	rels := []types.RelationshipRecord{
		{
			FromID: "aaaaaaaaaaaaaaaa",
			ToID:   "scope:operation:method:ruby:app/controllers/users_controller.rb:create",
			Kind:   "CONTAINS",
		},
		{
			FromID: "cccccccccccccccc",
			ToID:   "scope:operation:method:ruby:app/controllers/posts_controller.rb:create",
			Kind:   "CONTAINS",
		},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("users#create: ToID=%s, want bbbbbbbbbbbbbbbb", rels[0].ToID)
	}
	if rels[1].ToID != "dddddddddddddddd" {
		t.Fatalf("posts#create: ToID=%s, want dddddddddddddddd", rels[1].ToID)
	}
	if stats.Rewritten != 2 {
		t.Fatalf("expected 2 rewrites, got %+v", stats)
	}
}

// Issue #144 — Java class CONTAINS edges use the same Format-A structural
// ref pattern as Ruby. Two Java files each define a class with a same-named
// method (`save`); the CONTAINS edges must rewrite to the file-local method.
func TestReferences_JavaClassContainsStructuralRef(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "UserRepository", "src/main/java/UserRepository.java"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "save", "src/main/java/UserRepository.java"),
		entAt("cccccccccccccccc", "SCOPE.Component", "PostRepository", "src/main/java/PostRepository.java"),
		entAt("dddddddddddddddd", "SCOPE.Operation", "save", "src/main/java/PostRepository.java"),
	}
	rels := []types.RelationshipRecord{
		{
			FromID: "aaaaaaaaaaaaaaaa",
			ToID:   "scope:operation:method:java:src/main/java/UserRepository.java:save",
			Kind:   "CONTAINS",
		},
		{
			FromID: "cccccccccccccccc",
			ToID:   "scope:operation:method:java:src/main/java/PostRepository.java:save",
			Kind:   "CONTAINS",
		},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("UserRepository#save: ToID=%s, want bbbbbbbbbbbbbbbb", rels[0].ToID)
	}
	if rels[1].ToID != "dddddddddddddddd" {
		t.Fatalf("PostRepository#save: ToID=%s, want dddddddddddddddd", rels[1].ToID)
	}
	if stats.Rewritten != 2 {
		t.Fatalf("expected 2 rewrites, got %+v", stats)
	}
}

// Issue #144 — Python class CONTAINS edges use Format-A structural refs.
// Python emits methods with class-qualified Name "Foo.<method>" (issue #45),
// so the structural-ref tail carries the dotted form. Two files each define
// `Foo.save` — edges must rewrite to the file-local method.
func TestReferences_PythonClassContainsStructuralRef(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "UserStore", "app/users.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "UserStore.save", "app/users.py"),
		entAt("cccccccccccccccc", "SCOPE.Component", "PostStore", "app/posts.py"),
		entAt("dddddddddddddddd", "SCOPE.Operation", "PostStore.save", "app/posts.py"),
	}
	rels := []types.RelationshipRecord{
		{
			FromID: "aaaaaaaaaaaaaaaa",
			ToID:   "scope:operation:method:python:app/users.py:UserStore.save",
			Kind:   "CONTAINS",
		},
		{
			FromID: "cccccccccccccccc",
			ToID:   "scope:operation:method:python:app/posts.py:PostStore.save",
			Kind:   "CONTAINS",
		},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("UserStore.save: ToID=%s, want bbbbbbbbbbbbbbbb", rels[0].ToID)
	}
	if rels[1].ToID != "dddddddddddddddd" {
		t.Fatalf("PostStore.save: ToID=%s, want dddddddddddddddd", rels[1].ToID)
	}
	if stats.Rewritten != 2 {
		t.Fatalf("expected 2 rewrites, got %+v", stats)
	}
}

// Issue #148 — Go same-package method dispatch.
//
// Inside chi's own files, `(mx *Mux) Mount` calls `mx.handle(...)` which the
// Go extractor emits as CALLS edge ToID="handle" with
// Properties["receiver_type"]="Mux". Without same-package method dispatch
// this bare-name call collides with same-named methods on unrelated types
// and lands in bug-resolver. The resolver must use the receiver_type stamp
// plus the source file's package directory to pin the call to
// `<package>/Mux.handle`.
func TestReferencesEmbedded_GoSamePackageMethodDispatch(t *testing.T) {
	records := []types.EntityRecord{
		// Method receiver type lives in mux.go; methods Mount + handle in tree.go
		// — same package directory ("chi"). The receiver_type+pkg lookup must
		// span sibling files in the same directory, not just the caller file.
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Schema", Name: "Mux", SourceFile: "chi/mux.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Operation", Name: "Mux.handle", SourceFile: "chi/tree.go", Language: "go"},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "Mux.Mount",
			SourceFile: "chi/tree.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "handle",
					Kind:       "CALLS",
					Properties: map[string]string{"language": "go", "receiver_type": "Mux"},
				},
			},
		},
		// Decoy: an unrelated type with a same-named method in a DIFFERENT
		// package directory. Without the package-scoped index a global
		// bare-name lookup would either pick this entity or flag ambiguity.
		{ID: "dddddddddddddddd", Kind: "SCOPE.Operation", Name: "OtherType.handle", SourceFile: "other/store.go", Language: "go"},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	got := records[2].Relationships[0].ToID
	if got != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("issue #148: same-package Mux.handle not resolved; ToID=%s, want bbbbbbbbbbbbbbbb", got)
	}
	if stats.Rewritten < 1 {
		t.Fatalf("expected >=1 rewrite, got %+v", stats)
	}
}

// Issue #148 negative case — receiver_type stamp without a same-package
// match must NOT resolve to a foreign-package method of the same name.
func TestReferencesEmbedded_GoSamePackageMethodDispatch_NoFalseBind(t *testing.T) {
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Operation", Name: "OtherType.handle", SourceFile: "other/store.go", Language: "go"},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "Mux.Mount",
			SourceFile: "chi/tree.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "handle",
					Kind:       "CALLS",
					Properties: map[string]string{"language": "go", "receiver_type": "Mux"},
				},
			},
		},
	}
	idx := BuildIndex(records)
	_ = ReferencesEmbedded(records, idx)
	got := records[1].Relationships[0].ToID
	if got == "aaaaaaaaaaaaaaaa" {
		t.Fatalf("issue #148: foreign-package OtherType.handle wrongly bound to Mux.handle call")
	}
}

// Refs #44 — Go cross-file same-package DEPENDS_ON to bare struct type.
//
// The Go extractor emits a DEPENDS_ON edge from each method to its receiver
// type with ToID set to the bare type name (e.g. "Server"). When the struct
// is defined in a sibling file inside the same package directory, the global
// byName lookup either misses or flips to ambiguous because the same struct
// name appears in multiple packages (the dominant grpc-go-examples residual).
// The resolver must use the caller's package directory plus the bare ToID to
// pin the binding to the same-package Component entity.
func TestReferencesEmbedded_GoSamePackageComponentDispatch(t *testing.T) {
	records := []types.EntityRecord{
		// Struct `Server` defined in server.go, method `Serve` in serve.go —
		// both inside the `grpc/examples/helloworld` package directory.
		{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Component", Subtype: "struct",
			Name: "Server", SourceFile: "grpc/examples/helloworld/server.go", Language: "go"},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "Server.Serve",
			SourceFile: "grpc/examples/helloworld/serve.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "Server",
					Kind:       "DEPENDS_ON",
					Properties: map[string]string{"language": "go"},
				},
			},
		},
		// Decoy: a different `Server` struct in a foreign package directory.
		// Without package scoping, byName flips to ambiguous and the
		// DEPENDS_ON edge is left as a bug-resolver stub.
		{ID: "dddddddddddddddd", Kind: "SCOPE.Component", Subtype: "struct",
			Name: "Server", SourceFile: "grpc/examples/route_guide/server.go", Language: "go"},
	}
	idx := BuildIndex(records)
	_ = ReferencesEmbedded(records, idx)
	got := records[1].Relationships[0].ToID
	if got != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("Refs #44 byPackageComponent: same-package Server not resolved; ToID=%s, want aaaaaaaaaaaaaaaa", got)
	}
}

// Refs #44 negative — when no same-package component matches, the resolver
// must NOT bind to a foreign-package component of the same name; the edge
// stays unmatched / ambiguous so it can be diagnosed instead of silently
// pointing at the wrong struct.
func TestReferencesEmbedded_GoSamePackageComponentDispatch_NoFalseBind(t *testing.T) {
	records := []types.EntityRecord{
		// Only definition of `Server` lives in a DIFFERENT package directory
		// from the caller. The byPackageComponent[caller_pkg] bucket will
		// miss, the fallback rewriteOne hits byName which sees a single
		// candidate — but THAT outcome is acceptable when it's the only
		// candidate. Here we add a second decoy so byName flips ambiguous,
		// guaranteeing that the foreign-package entity is NOT silently
		// chosen by the package fast-path.
		{ID: "dddddddddddddddd", Kind: "SCOPE.Component", Subtype: "struct",
			Name: "Server", SourceFile: "foreign/pkg_a/server.go", Language: "go"},
		{ID: "eeeeeeeeeeeeeeee", Kind: "SCOPE.Component", Subtype: "struct",
			Name: "Server", SourceFile: "foreign/pkg_b/server.go", Language: "go"},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "Caller.Serve",
			SourceFile: "grpc/examples/helloworld/serve.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "Server",
					Kind:       "DEPENDS_ON",
					Properties: map[string]string{"language": "go"},
				},
			},
		},
	}
	idx := BuildIndex(records)
	_ = ReferencesEmbedded(records, idx)
	got := records[2].Relationships[0].ToID
	if got == "dddddddddddddddd" || got == "eeeeeeeeeeeeeeee" {
		t.Fatalf("Refs #44 byPackageComponent: foreign-package Server wrongly bound: ToID=%s", got)
	}
}

// Refs #44 ambiguity sentinel — two same-named structs inside the SAME
// package directory must trip the blank-string sentinel and leave the stub
// alone rather than picking an arbitrary overload.
func TestReferencesEmbedded_GoSamePackageComponentDispatch_AmbiguousInPkg(t *testing.T) {
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Component", Subtype: "struct",
			Name: "Server", SourceFile: "pkg/a.go", Language: "go"},
		{ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Component", Subtype: "struct",
			Name: "Server", SourceFile: "pkg/b.go", Language: "go"},
		{
			ID:         "cccccccccccccccc",
			Kind:       "SCOPE.Operation",
			Name:       "Caller.Use",
			SourceFile: "pkg/c.go",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     "cccccccccccccccc",
					ToID:       "Server",
					Kind:       "DEPENDS_ON",
					Properties: map[string]string{"language": "go"},
				},
			},
		},
	}
	idx := BuildIndex(records)
	_ = ReferencesEmbedded(records, idx)
	got := records[2].Relationships[0].ToID
	if got == "aaaaaaaaaaaaaaaa" || got == "bbbbbbbbbbbbbbbb" {
		t.Fatalf("Refs #44 byPackageComponent: ambiguous in-pkg sentinel ignored; ToID=%s", got)
	}
	if got != "Server" {
		t.Fatalf("Refs #44 byPackageComponent: expected unresolved stub 'Server', got %s", got)
	}
}

// Issue #432 — testmap unknown-prod-file marker (`scope:operation:?#<qname>`).
// The cross-language test→production extractor emits this shape when it
// cannot infer the production file for a call inside a test body. With no
// graph entity to bind to, the stub MUST be classified DispositionDynamic
// instead of bug-extractor (it is, by design, a heuristic ref that drives
// downstream coverage analysis — not a missing entity).
func TestDisposition_TestmapUnknownProdFile_IsDynamic(t *testing.T) {
	rels := []types.RelationshipRecord{{
		FromID:     "0000000000000000",
		ToID:       "scope:operation:?#requests.adapters.HTTPAdapter",
		Kind:       "TESTS",
		Properties: map[string]string{"language": "python"},
	}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, nil)
	if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
		t.Fatalf("expected 1 dynamic for scope:operation:?#…, got %+v",
			stats.DispositionCounts)
	}
	if got := stats.DispositionCounts[DispositionBugExtractor]; got != 0 {
		t.Fatalf("expected 0 bug-extractor for scope:operation:?#…, got %+v",
			stats.DispositionCounts)
	}
}

// Issue #432 — when the testmap "?" form's qname matches a unique
// QualifiedName in the graph, the structural-ref resolver should bind it
// to that entity instead of leaving it as Dynamic. This recovers a
// resolution credit for the small fraction of test-body references that
// happen to use the entity's full QualifiedName verbatim.
func TestReferences_TestmapUnknownProdFile_QnameRewrite(t *testing.T) {
	entities := []types.EntityRecord{{
		ID:            "aaaaaaaaaaaaaaaa",
		Kind:          "Operation",
		Name:          "extract_cookies_to_jar",
		QualifiedName: "extract_cookies_to_jar",
		SourceFile:    "src/requests/cookies.py",
	}}
	rels := []types.RelationshipRecord{{
		FromID:     "0000000000000000",
		ToID:       "scope:operation:?#extract_cookies_to_jar",
		Kind:       "TESTS",
		Properties: map[string]string{"language": "python"},
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("expected qname rewrite, ToID=%s", rels[0].ToID)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("expected ToRewritten=1, got %+v", stats)
	}
}

// Issue #432 — Python `from .compat import urlparse` reaches the resolver
// as a bare ToID `.compat.urlparse` (the leading dot is preserved by the
// extractor so the resolver sees the relative form). These are intra-package
// references the static resolver can't bind without project-layout
// awareness; classify them DispositionDynamic to keep the metric honest
// instead of inflating bug-resolver. Mirrors the rationale for
// scope:component:import:local: stubs that the cross-language imports
// extractor emits for the same shape.
func TestDisposition_PythonRelativeImport_IsDynamic(t *testing.T) {
	// Two SCOPE.Component placeholders sharing the relative-import name —
	// the shape produced by the Python extractor when multiple files write
	// `from .compat import urlparse`. Bare-name lookup is ambiguous and
	// the leaf exists in the graph → without the new pattern the edge
	// lands in DispositionBugResolver.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", ".compat.urlparse", "src/requests/auth.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Component", ".compat.urlparse", "src/requests/adapters.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID:     "0000000000000000",
		ToID:       ".compat.urlparse",
		Kind:       "IMPORTS",
		Properties: map[string]string{"language": "python"},
	}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, nil)
	if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
		t.Fatalf("expected 1 dynamic for .compat.urlparse, got %+v",
			stats.DispositionCounts)
	}
	if got := stats.DispositionCounts[DispositionBugResolver]; got != 0 {
		t.Fatalf("expected 0 bug-resolver for .compat.urlparse, got %+v",
			stats.DispositionCounts)
	}
}

// TestLooksLikeSourceFilePath_BasenameOnly verifies that basename-only
// source-file paths (root-level files like Package.swift, root main.go,
// root index.ts) are accepted, while non-source basenames (Makefile,
// Dockerfile) are still rejected. Regression coverage for issue #491.
func TestLooksLikeSourceFilePath_BasenameOnly(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Basename-only source files — must be accepted (#491).
		{"Package.swift", true},
		{"main.go", true},
		{"index.ts", true},
		// Sub-path source files — must still be accepted.
		{"a/b/Package.swift", true},
		{"src/main.go", true},
		// Non-source basenames — must still be rejected.
		{"Makefile", false},
		{"Dockerfile", false},
		// Existing guards still hold.
		{"", false},
		{"/abs/path/main.go", false},
		{"scope:component:foo", false},
	}
	for _, tc := range cases {
		if got := looksLikeSourceFilePath(tc.in); got != tc.want {
			t.Errorf("looksLikeSourceFilePath(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

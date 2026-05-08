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
